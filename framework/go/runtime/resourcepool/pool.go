package resourcepool

import (
	"container/heap"
	"context"
	"errors"
	"sync"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

const (
	defaultPoolName         = "resource-pool"
	maxWorkersPerGroup      = 64
	maxQueueCapacity        = 100000
	minPriority             = -1000000
	maxPriority             = 1000000
	defaultMaxPriorityBurst = 8
	maxPriorityBurst        = 1000
)

type Pool struct {
	mu           sync.RWMutex
	name         string
	groups       map[Group]*groupRuntime
	order        []Group
	started      bool
	closed       bool
	cancel       context.CancelCauseFunc
	done         chan struct{}
	workerWait   sync.WaitGroup
	shutdownOnce sync.Once
	doneOnce     sync.Once
	clock        frameworkclock.Clock
}

type groupRuntime struct {
	mu            sync.Mutex
	config        GroupConfig
	queue         priorityQueue
	entries       map[string]*jobEntry
	notify        chan struct{}
	closed        bool
	sequence      uint64
	running       int
	completed     uint64
	failed        uint64
	canceled      uint64
	rejected      uint64
	clock         frameworkclock.Clock
	priorityBurst int
}

func New(configs []GroupConfig, options Options) (*Pool, error) {
	if len(configs) == 0 {
		return nil, ErrInvalidConfig
	}
	name := options.Name
	if name == "" {
		name = defaultPoolName
	}
	if !namePattern.MatchString(name) {
		return nil, ErrInvalidConfig
	}
	clock := options.Clock
	if clock == nil {
		clock = frameworkclock.System{}
	}
	pool := &Pool{name: name, groups: make(map[Group]*groupRuntime, len(configs)), done: make(chan struct{}), clock: clock}
	for _, config := range configs {
		if config.MaxPriorityBurst == 0 {
			config.MaxPriorityBurst = defaultMaxPriorityBurst
		}
		if !namePattern.MatchString(string(config.Name)) || config.Workers < 1 || config.Workers > maxWorkersPerGroup ||
			config.QueueCapacity < 1 || config.QueueCapacity > maxQueueCapacity ||
			config.MaxPriorityBurst < 1 || config.MaxPriorityBurst > maxPriorityBurst {
			return nil, ErrInvalidConfig
		}
		if _, exists := pool.groups[config.Name]; exists {
			return nil, ErrInvalidConfig
		}
		pool.groups[config.Name] = &groupRuntime{
			config: config, queue: make(priorityQueue, 0, config.QueueCapacity), entries: make(map[string]*jobEntry),
			notify: make(chan struct{}, 1), clock: clock,
		}
		pool.order = append(pool.order, config.Name)
	}
	return pool, nil
}

func (pool *Pool) Name() string {
	if pool == nil {
		return ""
	}
	return pool.name
}

func (pool *Pool) Start(parent context.Context) error {
	if pool == nil || parent == nil {
		return ErrInvalidConfig
	}
	if err := parent.Err(); err != nil {
		return err
	}
	pool.mu.Lock()
	if pool.closed {
		pool.mu.Unlock()
		return ErrClosed
	}
	if pool.started {
		pool.mu.Unlock()
		return nil
	}
	root, cancel := context.WithCancelCause(parent)
	pool.started = true
	pool.cancel = cancel
	for _, name := range pool.order {
		group := pool.groups[name]
		for worker := 0; worker < group.config.Workers; worker++ {
			pool.workerWait.Add(1)
			go pool.worker(root, group)
		}
	}
	pool.mu.Unlock()
	go func() {
		<-root.Done()
		pool.initiateClose(context.Cause(root))
	}()
	go func() {
		pool.workerWait.Wait()
		pool.doneOnce.Do(func() { close(pool.done) })
	}()
	return nil
}

func (pool *Pool) Ready(context.Context) error {
	if pool == nil {
		return ErrInvalidConfig
	}
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	if pool.closed {
		return ErrClosed
	}
	if !pool.started {
		return ErrNotStarted
	}
	return nil
}

func (pool *Pool) Submit(ctx context.Context, job Job) (*Future, error) {
	if pool == nil || ctx == nil || !jobPattern.MatchString(job.ID) || !namePattern.MatchString(string(job.Group)) ||
		job.Execute == nil || job.Priority < minPriority || job.Priority > maxPriority {
		return nil, ErrInvalidJob
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pool.mu.RLock()
	started, closed := pool.started, pool.closed
	group := pool.groups[job.Group]
	pool.mu.RUnlock()
	if closed {
		return nil, ErrClosed
	}
	if !started {
		return nil, ErrNotStarted
	}
	if group == nil {
		return nil, ErrInvalidJob
	}
	return group.submit(ctx, job)
}

func (pool *Pool) Acquire(ctx context.Context, request LeaseRequest) (*Lease, error) {
	if pool == nil || ctx == nil || !jobPattern.MatchString(request.ID) || !request.Group.Valid() ||
		request.Priority < minPriority || request.Priority > maxPriority {
		return nil, ErrInvalidJob
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lease := &Lease{
		request: request, acquired: make(chan struct{}), release: make(chan struct{}),
	}
	future, err := pool.Submit(context.WithoutCancel(ctx), Job{
		ID: request.ID, Group: request.Group, Priority: request.Priority,
		Execute: func(executionCtx context.Context) error {
			lease.mu.Lock()
			lease.ctx = executionCtx
			lease.mu.Unlock()
			if !lease.state.CompareAndSwap(leasePending, leaseAcquired) {
				return ErrCanceled
			}
			close(lease.acquired)
			select {
			case <-lease.release:
				return nil
			case <-executionCtx.Done():
				lease.state.CompareAndSwap(leaseAcquired, leaseRevoked)
				return context.Cause(executionCtx)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	lease.future = future
	select {
	case <-lease.acquired:
		return lease, nil
	case <-future.done:
		result, waitErr := future.Wait(context.Background())
		if waitErr != nil {
			return nil, waitErr
		}
		if result.Err == nil {
			return nil, ErrCanceled
		}
		return nil, result.Err
	case <-ctx.Done():
		if lease.state.CompareAndSwap(leasePending, leaseRevoked) {
			future.Cancel()
			return nil, ctx.Err()
		}
		if lease.state.Load() == leaseAcquired {
			<-lease.acquired
			return lease, nil
		}
		return nil, ctx.Err()
	}
}

func (pool *Pool) Close(ctx context.Context) error {
	if pool == nil || ctx == nil {
		return ErrInvalidConfig
	}
	pool.mu.RLock()
	started := pool.started
	pool.mu.RUnlock()
	pool.initiateClose(ErrClosed)
	if !started {
		pool.doneOnce.Do(func() { close(pool.done) })
	}
	select {
	case <-pool.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (pool *Pool) Stop(ctx context.Context) error { return pool.Close(ctx) }

func (pool *Pool) Snapshot() Snapshot {
	if pool == nil {
		return Snapshot{}
	}
	pool.mu.RLock()
	result := Snapshot{Name: pool.name, Started: pool.started, Closed: pool.closed, Groups: make([]GroupSnapshot, 0, len(pool.order))}
	groups := make([]*groupRuntime, 0, len(pool.order))
	for _, name := range pool.order {
		groups = append(groups, pool.groups[name])
	}
	pool.mu.RUnlock()
	for _, group := range groups {
		group.mu.Lock()
		result.Groups = append(result.Groups, GroupSnapshot{
			Name: group.config.Name, Workers: group.config.Workers, QueueCapacity: group.config.QueueCapacity,
			MaxPriorityBurst: group.config.MaxPriorityBurst,
			Queued:           group.queue.Len(), Running: group.running, Completed: group.completed,
			Failed: group.failed, Canceled: group.canceled, Rejected: group.rejected,
		})
		group.mu.Unlock()
	}
	return result
}

func (pool *Pool) initiateClose(cause error) {
	if pool == nil {
		return
	}
	pool.shutdownOnce.Do(func() {
		if cause == nil {
			cause = ErrClosed
		}
		pool.mu.Lock()
		pool.closed = true
		cancel := pool.cancel
		groups := make([]*groupRuntime, 0, len(pool.order))
		for _, name := range pool.order {
			groups = append(groups, pool.groups[name])
		}
		pool.mu.Unlock()
		for _, group := range groups {
			group.close(cause)
		}
		if cancel != nil {
			cancel(cause)
		}
	})
}

func (pool *Pool) worker(root context.Context, group *groupRuntime) {
	defer pool.workerWait.Done()
	for {
		entry, closed := group.take()
		if entry != nil {
			pool.execute(group, entry)
			continue
		}
		if closed {
			return
		}
		select {
		case <-group.notify:
		case <-root.Done():
		}
	}
}

func (pool *Pool) execute(group *groupRuntime, entry *jobEntry) {
	startedAt := pool.clock.Now().UTC()
	err := executeSafely(entry.ctx, entry.job.Execute)
	cause := context.Cause(entry.ctx)
	if cause != nil && (err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		err = cause
	}
	if errors.Is(err, ErrCanceled) {
		err = ErrCanceled
	}
	result := Result{JobID: entry.job.ID, Group: entry.job.Group, StartedAt: startedAt, FinishedAt: pool.clock.Now().UTC(), Err: err}
	entry.cancel(context.Canceled)
	if entry.stopContextWatch != nil {
		entry.stopContextWatch()
	}
	group.finish(entry, result)
}

func (group *groupRuntime) submit(parent context.Context, job Job) (*Future, error) {
	group.mu.Lock()
	if group.closed {
		group.mu.Unlock()
		return nil, ErrClosed
	}
	if _, exists := group.entries[job.ID]; exists {
		group.mu.Unlock()
		return nil, ErrDuplicateJob
	}
	if group.queue.Len() >= group.config.QueueCapacity {
		group.rejected++
		group.mu.Unlock()
		return nil, ErrQueueFull
	}
	ctx, cancel := context.WithCancelCause(parent)
	future := &Future{done: make(chan struct{})}
	group.sequence++
	entry := &jobEntry{job: job, ctx: ctx, cancel: cancel, future: future, sequence: group.sequence, index: -1, state: jobQueued}
	future.cancel = func() bool { return group.cancel(job.ID, ErrCanceled) }
	group.entries[job.ID] = entry
	heap.Push(&group.queue, entry)
	entry.stopContextWatch = context.AfterFunc(parent, func() { group.cancel(job.ID, context.Cause(parent)) })
	group.mu.Unlock()
	group.signal()
	return future, nil
}

func (group *groupRuntime) take() (*jobEntry, bool) {
	group.mu.Lock()
	defer group.mu.Unlock()
	if group.queue.Len() == 0 {
		return nil, group.closed
	}
	var entry *jobEntry
	if group.priorityBurst >= group.config.MaxPriorityBurst && group.queue.Len() > 1 {
		entry = popOldest(&group.queue)
		group.priorityBurst = 0
	} else {
		entry = heap.Pop(&group.queue).(*jobEntry)
		group.priorityBurst++
	}
	entry.state = jobRunning
	group.running++
	return entry, false
}

func (group *groupRuntime) cancel(id string, cause error) bool {
	if cause == nil {
		cause = ErrCanceled
	}
	group.mu.Lock()
	entry := group.entries[id]
	if entry == nil || entry.cancelRequested {
		group.mu.Unlock()
		return false
	}
	entry.cancelRequested = true
	if entry.state == jobRunning {
		entry.cancel(cause)
		group.mu.Unlock()
		return true
	}
	removeEntry(&group.queue, entry)
	delete(group.entries, id)
	group.canceled++
	result := Result{JobID: entry.job.ID, Group: entry.job.Group, FinishedAt: group.clock.Now().UTC(), Err: cause}
	entry.cancel(cause)
	group.mu.Unlock()
	entry.future.complete(result)
	group.signal()
	return true
}

func (group *groupRuntime) finish(entry *jobEntry, result Result) {
	group.mu.Lock()
	delete(group.entries, entry.job.ID)
	group.running--
	switch {
	case errors.Is(result.Err, ErrCanceled) || errors.Is(result.Err, ErrClosed) || errors.Is(result.Err, context.Canceled):
		group.canceled++
	case result.Err != nil:
		group.failed++
	default:
		group.completed++
	}
	group.mu.Unlock()
	entry.future.complete(result)
	group.signal()
}

func (group *groupRuntime) close(cause error) {
	group.mu.Lock()
	group.closed = true
	queued := make([]*jobEntry, 0, group.queue.Len())
	for group.queue.Len() > 0 {
		entry := heap.Pop(&group.queue).(*jobEntry)
		delete(group.entries, entry.job.ID)
		entry.cancelRequested = true
		entry.cancel(cause)
		queued = append(queued, entry)
		group.canceled++
	}
	for _, entry := range group.entries {
		if entry.state == jobRunning && !entry.cancelRequested {
			entry.cancelRequested = true
			entry.cancel(cause)
		}
	}
	group.mu.Unlock()
	for _, entry := range queued {
		entry.future.complete(Result{JobID: entry.job.ID, Group: entry.job.Group, FinishedAt: group.clock.Now().UTC(), Err: cause})
	}
	group.signal()
}

func (group *groupRuntime) signal() {
	select {
	case group.notify <- struct{}{}:
	default:
	}
}

func executeSafely(ctx context.Context, execute ExecuteFunc) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrJobPanic
		}
	}()
	return execute(ctx)
}
