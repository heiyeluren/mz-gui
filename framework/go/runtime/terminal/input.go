package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"
)

var (
	ErrInputChanged   = errors.New("终端输入版本已变化，本次未写入")
	ErrInputUncertain = errors.New("终端输入结果未确认，不可自动重发")
)

// WriteIfInputUnchanged 仅在输入版本仍等于 expected 时尝试一次设备写入。
// 普通 Write 使用同一排他边界；纯设备应答不占用户输入版本，混合数据仍计数。
// ErrInputChanged 保证本次没有写入字节。
// 一旦尝试设备写，版本先递增且不回退，返回错误也不代表未发送。
// 返回值是该次尝试消费的版本或拒绝时的当前版本；未能读取会话时为 0。
// 此计数只属于当前终端实例，不代替调用方的持久任务身份和发送回执。
func (manager *Manager) WriteIfInputUnchanged(ctx context.Context, id string, expected uint64, data []byte) (uint64, error) {
	return manager.writeInput(ctx, id, &expected, data)
}

func (manager *Manager) writeInput(ctx context.Context, id string, expected *uint64, data []byte) (uint64, error) {
	return manager.writeInputSource(ctx, id, expected, data, false)
}

func (manager *Manager) writeInputSource(ctx context.Context, id string, expected *uint64, data []byte, generated bool) (uint64, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	if len(data) == 0 || len(data) > MaxInputBytes {
		return 0, ErrInvalidArgument
	}
	session, err := manager.session(id)
	if err != nil {
		return 0, err
	}
	if err := session.acquireIO(ctx, true); err != nil {
		return session.snapshotInfo().InputRevision, err
	}
	session.mu.Lock()
	revision := session.info.InputRevision
	// 等待期间及获得 I/O 所有权后都重新核对状态和 context。
	err = ctx.Err()
	if err == nil && session.info.State != StateRunning {
		err = ErrSessionClosed
	}
	if err == nil && expected != nil && *expected != revision {
		err = ErrInputChanged
	}
	if err != nil {
		session.mu.Unlock()
		session.releaseIO()
		return revision, err
	}
	// 用同一份不可变副本分类和写入，避免调用方改变缓冲后绕过输入版本。
	owned := append([]byte(nil), data...)
	countsAsInput := !generated && (expected != nil || !terminalReplyOnly(owned))
	if countsAsInput && revision == math.MaxUint64 {
		session.mu.Unlock()
		session.releaseIO()
		return revision, ErrInvalidArgument
	}
	if countsAsInput {
		revision++
		session.info.InputRevision = revision
		for _, b := range owned {
			if b == '\r' || b == '\n' {
				session.info.LastSubmitInputRevision = revision
				session.info.LastSubmitAt = manager.now()
				break
			}
		}
		if expected != nil || !terminalNavigationOnly(owned) {
			session.info.LastNonNavigationInputRevision = revision
		}
	}
	session.info.UpdatedAt = manager.now()
	session.mu.Unlock()

	done := make(chan struct{})
	var finished bool
	var writeErr error
	go func() {
		err := writeDevice(ctx, session.device, owned)
		session.mu.Lock()
		finished, writeErr = true, err
		if session.writeUncertain {
			session.writeUncertain = false
			session.notifyInputChangedLocked()
		}
		session.mu.Unlock()
		// 设备确实返回且 deadline 回调收尾后才释放排他，不等待调用方。
		session.releaseIO()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		session.mu.Lock()
		if !finished {
			session.writeUncertain = true
			session.notifyInputChangedLocked()
			session.mu.Unlock()
			return revision, errors.Join(ErrInputUncertain, ctx.Err())
		}
		session.mu.Unlock()
	}
	if writeErr != nil {
		return revision, fmt.Errorf("写入终端: %w", writeErr)
	}
	return revision, nil
}

// acquireIO 使用会话内信号量；等锁时可取消，uncertain 会唤醒已排队的写调用。
func (session *managedSession) acquireIO(ctx context.Context, writing bool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		session.mu.RLock()
		state, uncertain, changed := session.info.State, session.writeUncertain, session.writeChanged
		session.mu.RUnlock()
		if state != StateRunning {
			return ErrSessionClosed
		}
		if writing && uncertain {
			return ErrInputUncertain
		}
		select {
		case session.ioGate <- struct{}{}:
			if err := ctx.Err(); err != nil {
				session.releaseIO()
				return err
			}
			session.mu.RLock()
			state, uncertain = session.info.State, session.writeUncertain
			session.mu.RUnlock()
			if state != StateRunning {
				session.releaseIO()
				return ErrSessionClosed
			}
			if writing && uncertain {
				session.releaseIO()
				return ErrInputUncertain
			}
			return nil
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (session *managedSession) releaseIO() { <-session.ioGate }

func (session *managedSession) notifyInputChangedLocked() {
	close(session.writeChanged)
	session.writeChanged = make(chan struct{})
}

type writeDeadlineSetter interface{ SetWriteDeadline(time.Time) error }

// writeDevice 只在设备真正返回后返回；调用方的 context 等待与 I/O 所有权由上一层管理。
func writeDevice(ctx context.Context, device Device, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if setter, ok := device.(writeDeadlineSetter); ok {
		if deadline, ok := ctx.Deadline(); ok {
			_ = setter.SetWriteDeadline(deadline)
		}
		callbackDone := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			defer close(callbackDone)
			_ = setter.SetWriteDeadline(time.Unix(1, 0))
		})
		defer func() {
			if !stop() {
				<-callbackDone
			}
			_ = setter.SetWriteDeadline(time.Time{})
		}()
	}
	count, err := device.Write(data)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			err = contextErr
		} else if errors.Is(err, os.ErrDeadlineExceeded) {
			if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
				err = context.DeadlineExceeded
			}
		}
	} else if count != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil && count > 0 {
		return errors.Join(ErrInputUncertain, err)
	}
	return err
}
