package terminal

import (
	"context"
	"strings"
	"time"
)

// Observer 接收独立于页面与有界回放缓存的生命周期、输出事件。
// 实现必须有界返回，不得执行网络请求或模型调用；持久队列由调用方实现。
// 不暴露 argv、环境变量、逐键输入或设备应答。
type Observer interface{ Observe(ObservationEvent) }
type ObserverFunc func(ObservationEvent)

func (f ObserverFunc) Observe(e ObservationEvent) { f(e) }

type ObservationEvent struct {
	Kind    string
	Session SessionInfo
	Tags    map[string]string
	At      time.Time
	Data    []byte
	Message string
}

// RecordObservation 让授权调用方追加已提交任务或已验证回执的注释；不读取逐键输入。
func (m *Manager) RecordObservation(ctx context.Context, id, name string, data []byte) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if !ValidScope(name) || len(data) > 1<<20 {
		return ErrInvalidArgument
	}
	s, err := m.session(id)
	if err != nil {
		return err
	}
	m.observe(ObservationEvent{Kind: "annotation", Message: name, Session: s.snapshotInfo(), Tags: s.observationTagsSnapshot(), Data: data, At: m.now()})
	return nil
}

// RecordLaunchFailure 保留进入实际进程启动前的准备失败，不创建 PTY 或进程。
func (m *Manager) RecordLaunchFailure(ctx context.Context, spec SessionSpec, message string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if !ValidScope(spec.Scope) || len(message) > 4096 {
		return ErrInvalidArgument
	}
	id, err := m.newID()
	if err != nil {
		return err
	}
	info := SessionInfo{SessionID: id, CWD: spec.CWD, Scope: spec.Scope, Title: strings.TrimSpace(spec.Title), State: "launch_failed", CreatedAt: m.now()}
	m.observe(ObservationEvent{Kind: "launch_requested", Session: info, Tags: spec.Observation, At: m.now()})
	m.observe(ObservationEvent{Kind: "launch_failed", Session: info, Tags: spec.Observation, At: m.now(), Message: message})
	return nil
}

func cloneObservationTags(tags map[string]string) map[string]string {
	if len(tags) > 32 {
		return nil
	}
	result := make(map[string]string, len(tags))
	for k, v := range tags {
		if len(k) <= 64 && len(v) <= 4096 {
			result[k] = v
		}
	}
	return result
}

func (m *Manager) observe(e ObservationEvent) {
	if m.observer == nil {
		return
	}
	// 观察者异常不能改变进程或终端的执行结果。
	defer func() { _ = recover() }()
	e.Data = append([]byte(nil), e.Data...)
	e.Tags = cloneObservationTags(e.Tags)
	e.Session = cloneSessionInfo(e.Session)
	m.observer.Observe(e)
}

func (m *Manager) observeCompletion(s *managedSession) {
	defer m.observationWG.Done()
	<-s.done
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-s.readerDone:
	case <-timer.C:
		m.observe(ObservationEvent{Kind: "gap", Session: s.snapshotInfo(), Tags: s.observationTagsSnapshot(), At: m.now(), Message: "进程已退出，终端尾部输出尚未排空"})
	}
	m.observe(ObservationEvent{Kind: "exited", Session: s.snapshotInfo(), Tags: s.observationTagsSnapshot(), At: m.now()})
}

func (s *managedSession) observationTagsSnapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneObservationTags(s.observationTags)
}

// AnnotateObservation 只补充通用关联标签，既不发送终端输入也不改变进程状态。
func (m *Manager) AnnotateObservation(ctx context.Context, id string, tags map[string]string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	s, err := m.session(id)
	if err != nil {
		return err
	}
	values := cloneObservationTags(tags)
	if values == nil {
		return ErrInvalidArgument
	}
	s.mu.Lock()
	changed := false
	next := cloneObservationTags(s.observationTags)
	for key, value := range values {
		if next[key] != value {
			next[key] = value
			changed = true
		}
	}
	if len(next) > 32 {
		s.mu.Unlock()
		return ErrInvalidArgument
	}
	if changed {
		s.observationTags = next
	}
	s.mu.Unlock()
	if changed {
		m.observe(ObservationEvent{Kind: "context_bound", Session: s.snapshotInfo(), Tags: next, At: m.now()})
	}
	return nil
}
