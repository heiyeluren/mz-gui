// Package resourcepool 提供按资源组隔离的有界并发执行运行时。
package resourcepool

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

var (
	ErrInvalidConfig = errors.New("资源池配置无效")
	ErrInvalidJob    = errors.New("资源池任务无效")
	ErrNotStarted    = errors.New("资源池尚未启动")
	ErrClosed        = errors.New("资源池已关闭")
	ErrQueueFull     = errors.New("资源组队列已满")
	ErrDuplicateJob  = errors.New("资源组任务 ID 重复")
	ErrCanceled      = errors.New("资源池任务已取消")
	ErrJobPanic      = errors.New("资源池任务发生异常")

	namePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	jobPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type Group string

const (
	GroupDefault  Group = "default"
	GroupCPU      Group = "cpu"
	GroupNetwork  Group = "network"
	GroupBrowser  Group = "browser"
	GroupTerminal Group = "terminal"
	GroupMedia    Group = "media"
)

func (group Group) Valid() bool {
	return namePattern.MatchString(string(group))
}

type GroupConfig struct {
	Name             Group
	Workers          int
	QueueCapacity    int
	MaxPriorityBurst int
}

type Options struct {
	Name  string
	Clock frameworkclock.Clock
}

type ExecuteFunc func(context.Context) error

type Job struct {
	ID       string
	Group    Group
	Priority int
	Execute  ExecuteFunc
}

type LeaseRequest struct {
	ID       string
	Group    Group
	Priority int
}

const (
	PriorityBackground  = -100
	PriorityNormal      = 0
	PriorityInteractive = 100
)

type Result struct {
	JobID      string
	Group      Group
	StartedAt  time.Time
	FinishedAt time.Time
	Err        error
}

type Future struct {
	done   chan struct{}
	cancel func() bool
	once   sync.Once
	mu     sync.RWMutex
	result Result
}

const (
	leasePending uint32 = iota
	leaseAcquired
	leaseReleased
	leaseRevoked
)

// Lease 在整个生命周期占用一个资源组 Worker，适用于终端等长生命周期资源。
type Lease struct {
	request  LeaseRequest
	acquired chan struct{}
	release  chan struct{}
	future   *Future
	state    atomic.Uint32
	mu       sync.RWMutex
	ctx      context.Context
}

func (lease *Lease) ID() string {
	if lease == nil {
		return ""
	}
	return lease.request.ID
}

func (lease *Lease) Group() Group {
	if lease == nil {
		return ""
	}
	return lease.request.Group
}

func (lease *Lease) Context() context.Context {
	if lease == nil {
		return nil
	}
	lease.mu.RLock()
	defer lease.mu.RUnlock()
	return lease.ctx
}

func (lease *Lease) Done() <-chan struct{} {
	if lease == nil || lease.future == nil {
		return nil
	}
	return lease.future.done
}

func (lease *Lease) Wait(ctx context.Context) (Result, error) {
	if lease == nil || lease.future == nil {
		return Result{}, ErrInvalidJob
	}
	return lease.future.Wait(ctx)
}

func (lease *Lease) Release() bool {
	if lease == nil || !lease.state.CompareAndSwap(leaseAcquired, leaseReleased) {
		return false
	}
	close(lease.release)
	return true
}

func (future *Future) Wait(ctx context.Context) (Result, error) {
	if future == nil || ctx == nil {
		return Result{}, ErrInvalidJob
	}
	select {
	case <-future.done:
		future.mu.RLock()
		defer future.mu.RUnlock()
		return future.result, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func (future *Future) Cancel() bool {
	return future != nil && future.cancel != nil && future.cancel()
}

func (future *Future) complete(result Result) {
	future.once.Do(func() {
		future.mu.Lock()
		future.result = result
		future.mu.Unlock()
		close(future.done)
	})
}

type GroupSnapshot struct {
	Name             Group  `json:"name"`
	Workers          int    `json:"workers"`
	QueueCapacity    int    `json:"queue_capacity"`
	MaxPriorityBurst int    `json:"max_priority_burst"`
	Queued           int    `json:"queued"`
	Running          int    `json:"running"`
	Completed        uint64 `json:"completed"`
	Failed           uint64 `json:"failed"`
	Canceled         uint64 `json:"canceled"`
	Rejected         uint64 `json:"rejected"`
}

type Snapshot struct {
	Name    string          `json:"name"`
	Started bool            `json:"started"`
	Closed  bool            `json:"closed"`
	Groups  []GroupSnapshot `json:"groups"`
}
