package terminal

import (
	"context"
	"errors"
	"time"
)

// ViewState 独立于 UI 生命周期维护完整终端状态。实现不能向 PTY 写回数据。
type ViewState interface {
	Write([]byte) error
	Resize(int, int) error
	Serialize() (string, error)
	Close()
}
type ViewStateFactory func(cols, rows int) (ViewState, error)

var ErrViewStateUnavailable = errors.New("终端画面状态暂不可恢复")

func (s *managedSession) appendOutput(data []byte, now time.Time) []byte {
	s.outputMu.Lock()
	defer s.outputMu.Unlock()
	if s.viewState != nil && s.viewErr == nil {
		s.viewErr = s.viewState.Write(data)
	}
	s.output.append(data, now)
	if state, ok := s.viewState.(interface{ TakeReplies() []byte }); ok {
		return state.TakeReplies()
	}
	return nil
}
func (s *managedSession) finishView() {
	s.outputMu.Lock()
	defer s.outputMu.Unlock()
	if f, ok := s.viewState.(interface{ Freeze() error }); ok && s.viewErr == nil {
		s.viewErr = f.Freeze()
	}
}
func (s *managedSession) closeView() {
	// 状态实例在会话内不可替换；Close 必须先中断辅助进程，不等待持输出锁的解析请求。
	if s.viewState != nil {
		s.viewState.Close()
	}
}

// ViewSnapshot 为终端画布返回原子恢复点；普通 Snapshot 保留原始帧语义供业务观察者消费。
func (m *Manager) ViewSnapshot(ctx context.Context, id string, cursor uint64) (Snapshot, error) {
	if err := contextError(ctx); err != nil {
		return Snapshot{}, err
	}
	s, err := m.readSession(id)
	if err != nil {
		return Snapshot{}, err
	}
	s.outputMu.Lock()
	defer s.outputMu.Unlock()
	output := s.output.snapshot(cursor, m.snapshotBytes)
	info := s.outputInfo()
	result := Snapshot{ScreenManaged: s.viewState != nil, Session: info, Frames: output.Frames, OldestCursor: output.OldestCursor, NextCursor: output.NextCursor, Truncated: output.Truncated, HasMore: output.HasMore}
	if s.viewState != nil && (cursor == 0 || output.Truncated) {
		if s.viewErr != nil {
			return Snapshot{}, ErrViewStateUnavailable
		}
		state, err := s.viewState.Serialize()
		if err != nil {
			return Snapshot{}, ErrViewStateUnavailable
		}
		s.output.mu.RLock()
		latest := s.output.sequence
		s.output.mu.RUnlock()
		result.ScreenState = state
		result.ScreenCursor = latest
	}
	result.Session.Cursor = result.NextCursor
	return result, nil
}

// 设备应答由唯一状态所有者生成；UI 回放不重复应答，隐藏页面也不会让 CLI 等待查询超时。
func (m *Manager) appendOutput(s *managedSession, data []byte) {
	replies := s.appendOutput(data, m.now())
	if m.observer != nil {
		m.observe(ObservationEvent{Kind: "output", Session: s.snapshotInfo(), Tags: s.observationTagsSnapshot(), At: m.now(), Data: data})
	}
	if len(replies) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, time.Second)
	defer cancel()
	if len(replies) <= MaxInputBytes {
		_, _ = m.writeInputSource(ctx, s.info.SessionID, nil, replies, true)
	}
}

// 子进程退出和 PTY 尾部读尽不是同一个时刻；尾部尚未处理完时不能提前停止消费。
func (s *managedSession) outputInfo() SessionInfo {
	info := s.snapshotInfo()
	if terminalState(info.State) && s.readerDone != nil {
		select {
		case <-s.readerDone:
		default:
			info.State = StateClosing
		}
	}
	return info
}
