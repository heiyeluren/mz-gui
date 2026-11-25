package terminal

import "time"

// readSession在同一管理锁内登记精确读取，避免新建回收与读取更新的LRU顺序交错。
// List只列举，不把所有历史会话误判为正在查看。
func (manager *Manager) readSession(id string) (*managedSession, error) {
	if id == "" {
		return nil, ErrInvalidArgument
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	session := manager.sessions[id]
	if session == nil {
		return nil, ErrSessionNotFound
	}
	now := manager.now()
	session.mu.Lock()
	if now.After(session.lastAccess) {
		session.lastAccess = now
	}
	session.mu.Unlock()
	return session, nil
}

// evictCompletedLocked仅由Create持管理写锁时调用，不增加清理goroutine或timer。
// 保留窗口只承诺终态缓存可回读的最短时间，不等于消费者已完成持久化。
func (manager *Manager) evictCompletedLocked(now time.Time) bool {
	var oldestID string
	var oldestAccess time.Time
	for id, session := range manager.sessions {
		session.mu.RLock()
		eligible := terminalState(session.info.State) && session.info.ExitedAt != nil && !now.Before(session.info.ExitedAt.Add(manager.completedRetentionGrace))
		lastAccess := session.lastAccess
		session.mu.RUnlock()
		if !eligible {
			continue
		}
		// 进程退出可能早于设备读到最后一帧；此时绝不能清掉ring。
		select {
		case <-session.readerDone:
		default:
			continue
		}
		if oldestID == "" || lastAccess.Before(oldestAccess) || (lastAccess.Equal(oldestAccess) && id < oldestID) {
			oldestID, oldestAccess = id, lastAccess
		}
	}
	if oldestID == "" {
		return false
	}
	manager.sessions[oldestID].closeView()
	delete(manager.sessions, oldestID)
	return true
}
