package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

type runtimeNode struct {
	id            string
	component     Component
	requires      []string
	timeouts      Timeouts
	failurePolicy FailurePolicy
	level         int
}

type Supervisor struct {
	opMu sync.Mutex
	mu   sync.RWMutex

	nodes             []runtimeNode
	states            map[string]ComponentSnapshot
	started           []int
	running           bool
	closed            bool
	degraded          bool
	sequence          uint64
	observedAt        time.Time
	clock             frameworkclock.Clock
	observer          Observer
	maxParallelStarts int
}

// New 保持旧调用方式，并把输入顺序转换为前一节点是后一节点必需依赖的线性 DAG。
func New(components []Component) (*Supervisor, error) {
	if len(components) == 0 {
		return nil, ErrInvalidArgument
	}
	nodes := make([]Node, 0, len(components))
	for index, component := range components {
		node := Node{Component: component, FailurePolicy: FailureRequired}
		if index > 0 && !isNilComponent(components[index-1]) {
			node.Requires = []string{components[index-1].Name()}
		}
		nodes = append(nodes, node)
	}
	supervisor, err := NewGraph(nodes, Options{})
	if errors.Is(err, ErrInvalidGraph) {
		return nil, ErrInvalidArgument
	}
	return supervisor, err
}

func NewGraph(nodes []Node, options Options) (*Supervisor, error) {
	if len(nodes) == 0 {
		return nil, ErrInvalidGraph
	}
	maxParallelStarts := options.MaxParallelStarts
	if maxParallelStarts < 0 {
		return nil, ErrInvalidGraph
	}
	if maxParallelStarts == 0 {
		maxParallelStarts = 1
	}
	clock := options.Clock
	if clock == nil {
		clock = frameworkclock.System{}
	}
	initialObservedAt := clock.Now().UTC()
	initialTimestamp, err := model.NewTimestamp(initialObservedAt)
	if err != nil {
		return nil, ErrInvalidGraph
	}
	defaults, err := normalizeTimeouts(options.DefaultTimeouts, Timeouts{
		Start: defaultPhaseTimeout, Ready: defaultPhaseTimeout, Quiesce: defaultPhaseTimeout, Stop: defaultPhaseTimeout,
	})
	if err != nil {
		return nil, err
	}
	ordered, err := buildGraph(nodes, defaults)
	if err != nil {
		return nil, err
	}
	states := make(map[string]ComponentSnapshot, len(ordered))
	for _, node := range ordered {
		componentID := model.ID(node.id)
		states[node.id] = ComponentSnapshot{
			ComponentID: componentID, State: StateCreated, ReasonCode: ReasonCreated, ObservedAt: initialTimestamp,
		}
	}
	return &Supervisor{
		nodes: ordered, states: states, clock: clock, observer: options.Observer, observedAt: initialObservedAt,
		maxParallelStarts: maxParallelStarts,
	}, nil
}

func (supervisor *Supervisor) Start(ctx context.Context) error {
	if supervisor == nil || ctx == nil {
		return ErrInvalidArgument
	}
	supervisor.opMu.Lock()
	defer supervisor.opMu.Unlock()
	if supervisor.closedValue() {
		return ErrClosed
	}
	if supervisor.runningValue() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	supervisor.setDegraded(false)
	maxLevel := 0
	for _, node := range supervisor.nodes {
		if node.level > maxLevel {
			maxLevel = node.level
		}
	}
	for level := 0; level <= maxLevel; level++ {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, supervisor.rollback(ctx))
		}
		indexes := make([]int, 0)
		for index, node := range supervisor.nodes {
			if node.level == level {
				indexes = append(indexes, index)
			}
		}
		if err := supervisor.startWave(ctx, indexes); err != nil {
			return errors.Join(err, supervisor.rollback(ctx))
		}
	}
	supervisor.setRunning(true)
	return nil
}

type nodeStartResult struct {
	index    int
	started  bool
	degraded bool
	err      error
}

func (supervisor *Supervisor) startWave(parent context.Context, indexes []int) error {
	if len(indexes) == 0 {
		return nil
	}
	waveContext, cancel := context.WithCancel(parent)
	defer cancel()
	jobs := make(chan int)
	results := make(chan nodeStartResult, len(indexes))
	workerCount := min(supervisor.maxParallelStarts, len(indexes))
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if waveContext.Err() != nil {
					return
				}
				result := supervisor.startNode(waveContext, index)
				results <- result
				if result.err != nil && supervisor.nodes[index].failurePolicy == FailureRequired {
					cancel()
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, index := range indexes {
			select {
			case jobs <- index:
			case <-waveContext.Done():
				return
			}
		}
	}()
	workers.Wait()
	close(results)
	ordered := make([]nodeStartResult, 0, len(indexes))
	for result := range results {
		ordered = append(ordered, result)
	}
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].index < ordered[right].index })
	var resultErr error
	for _, result := range ordered {
		if result.started {
			supervisor.started = append(supervisor.started, result.index)
		}
		if result.degraded {
			supervisor.setDegraded(true)
		}
		if result.err != nil {
			resultErr = errors.Join(resultErr, result.err)
		}
	}
	return resultErr
}

func (supervisor *Supervisor) startNode(ctx context.Context, index int) nodeStartResult {
	node := supervisor.nodes[index]
	if dependency := supervisor.firstUnavailableRequired(node); dependency != "" {
		dependencyErr := fmt.Errorf("%w: %s requires %s", ErrDependencyUnavailable, node.id, dependency)
		if node.failurePolicy == FailureOptional {
			supervisor.transition(ctx, index, StateDegraded, ReasonDependencyUnavailable)
			return nodeStartResult{index: index, degraded: true}
		}
		supervisor.transition(ctx, index, StateFailed, ReasonDependencyUnavailable)
		return nodeStartResult{index: index, err: dependencyErr}
	}
	supervisor.transition(ctx, index, StateStarting, ReasonStarting)
	if err := runPhase(ctx, node.timeouts.Start, node.component.Start); err != nil {
		if ctx.Err() != nil || node.failurePolicy == FailureRequired {
			supervisor.transition(ctx, index, StateFailed, ReasonStartFailed)
			return nodeStartResult{index: index, err: err}
		}
		supervisor.transition(ctx, index, StateDegraded, ReasonStartFailed)
		return nodeStartResult{index: index, degraded: true}
	}
	result := nodeStartResult{index: index, started: true}
	if err := runPhase(ctx, node.timeouts.Ready, node.component.Ready); err != nil {
		if ctx.Err() != nil || node.failurePolicy == FailureRequired {
			supervisor.transition(ctx, index, StateFailed, ReasonReadyFailed)
			result.err = err
			return result
		}
		supervisor.transition(ctx, index, StateDegraded, ReasonReadyFailed)
		result.degraded = true
		return result
	}
	supervisor.transition(ctx, index, StateReady, ReasonReady)
	return result
}

func (supervisor *Supervisor) Stop(ctx context.Context) error {
	if supervisor == nil || ctx == nil {
		return ErrInvalidArgument
	}
	supervisor.opMu.Lock()
	defer supervisor.opMu.Unlock()
	if supervisor.closedValue() {
		return nil
	}
	supervisor.setClosed(true)
	supervisor.setRunning(false)
	cleanupContext := context.WithoutCancel(ctx)
	var result error
	for position := len(supervisor.started) - 1; position >= 0; position-- {
		index := supervisor.started[position]
		node := supervisor.nodes[index]
		supervisor.transition(cleanupContext, index, StateQuiescing, ReasonQuiescing)
		if quiescer, ok := node.component.(Quiescer); ok {
			if err := runPhase(cleanupContext, node.timeouts.Quiesce, quiescer.Quiesce); err != nil {
				result = errors.Join(result, err)
				supervisor.setDegraded(true)
				supervisor.transition(cleanupContext, index, StateDegraded, ReasonQuiesceFailed)
			}
		}
	}
	result = errors.Join(result, supervisor.stopStarted(cleanupContext))
	return errors.Join(result, ctx.Err())
}

func (supervisor *Supervisor) Snapshot() Snapshot {
	if supervisor == nil {
		return Snapshot{}
	}
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	components := make([]ComponentSnapshot, 0, len(supervisor.nodes))
	for _, node := range supervisor.nodes {
		components = append(components, supervisor.states[node.id])
	}
	observedAt, _ := model.NewTimestamp(supervisor.observedAt)
	return Snapshot{
		Running: supervisor.running, Closed: supervisor.closed, Degraded: supervisor.degraded,
		ObservedAt: observedAt, Components: components,
	}
}

func (supervisor *Supervisor) rollback(parent context.Context) error {
	return supervisor.stopStarted(context.WithoutCancel(parent))
}

func (supervisor *Supervisor) stopStarted(parent context.Context) error {
	var result error
	for position := len(supervisor.started) - 1; position >= 0; position-- {
		index := supervisor.started[position]
		node := supervisor.nodes[index]
		supervisor.transition(parent, index, StateStopping, ReasonStopping)
		if err := runPhase(parent, node.timeouts.Stop, node.component.Stop); err != nil {
			result = errors.Join(result, err)
			supervisor.setDegraded(true)
			supervisor.transition(parent, index, StateFailed, ReasonStopFailed)
			continue
		}
		supervisor.transition(parent, index, StateStopped, ReasonStopped)
	}
	supervisor.started = nil
	supervisor.setRunning(false)
	return result
}

func (supervisor *Supervisor) firstUnavailableRequired(node runtimeNode) string {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	for _, dependency := range node.requires {
		if supervisor.states[dependency].State != StateReady {
			return dependency
		}
	}
	return ""
}

func (supervisor *Supervisor) transition(ctx context.Context, index int, state State, reason string) {
	now := supervisor.clock.Now().UTC()
	supervisor.mu.Lock()
	if !now.After(supervisor.observedAt) {
		now = supervisor.observedAt.Add(time.Nanosecond)
	}
	timestamp, _ := model.NewTimestamp(now)
	node := supervisor.nodes[index]
	componentID := node.id
	previous := supervisor.states[componentID]
	supervisor.sequence++
	event := Event{
		Sequence: supervisor.sequence, ComponentID: model.ID(componentID), From: previous.State, To: state,
		ReasonCode: reason, ObservedAt: timestamp,
	}
	supervisor.states[componentID] = ComponentSnapshot{
		ComponentID: event.ComponentID, State: state, ReasonCode: reason, ObservedAt: timestamp,
	}
	supervisor.observedAt = now
	observer := supervisor.observer
	supervisor.mu.Unlock()
	if observer != nil {
		notifyObserver(context.WithoutCancel(ctx), observer, event)
	}
}

func (supervisor *Supervisor) runningValue() bool {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	return supervisor.running
}

func (supervisor *Supervisor) closedValue() bool {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	return supervisor.closed
}

func (supervisor *Supervisor) setRunning(value bool) {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	supervisor.running = value
}

func (supervisor *Supervisor) setClosed(value bool) {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	supervisor.closed = value
}

func (supervisor *Supervisor) setDegraded(value bool) {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	supervisor.degraded = value
}

func buildGraph(nodes []Node, defaults Timeouts) ([]runtimeNode, error) {
	byOriginalID := make(map[string]int, len(nodes))
	componentIDs := make([]string, len(nodes))
	for index, node := range nodes {
		if isNilComponent(node.Component) {
			return nil, ErrInvalidGraph
		}
		componentID := node.Component.Name()
		if !componentNamePattern.MatchString(componentID) {
			return nil, ErrInvalidGraph
		}
		if _, exists := byOriginalID[componentID]; exists {
			return nil, ErrInvalidGraph
		}
		componentIDs[index] = componentID
		byOriginalID[componentID] = index
	}
	indegree := make([]int, len(nodes))
	dependents := make([][]int, len(nodes))
	normalized := make([]runtimeNode, len(nodes))
	dependenciesByIndex := make([][]string, len(nodes))
	for index, node := range nodes {
		policy := node.FailurePolicy
		if policy == "" {
			policy = FailureRequired
		}
		if policy != FailureRequired && policy != FailureOptional {
			return nil, ErrInvalidGraph
		}
		timeouts, err := normalizeTimeouts(node.Timeouts, defaults)
		if err != nil {
			return nil, err
		}
		dependencies := append(append([]string(nil), node.Requires...), node.OptionalRequires...)
		dependenciesByIndex[index] = append([]string(nil), dependencies...)
		seen := make(map[string]struct{}, len(dependencies))
		for _, dependency := range dependencies {
			dependencyIndex, exists := byOriginalID[dependency]
			if !exists || dependency == componentIDs[index] {
				return nil, ErrInvalidGraph
			}
			if _, exists := seen[dependency]; exists {
				return nil, ErrInvalidGraph
			}
			seen[dependency] = struct{}{}
			indegree[index]++
			dependents[dependencyIndex] = append(dependents[dependencyIndex], index)
		}
		normalized[index] = runtimeNode{
			id: componentIDs[index], component: node.Component, requires: append([]string(nil), node.Requires...),
			timeouts: timeouts, failurePolicy: policy,
		}
	}
	ready := make([]int, 0, len(nodes))
	for index, degree := range indegree {
		if degree == 0 {
			ready = append(ready, index)
		}
	}
	order := make([]int, 0, len(nodes))
	for len(ready) > 0 {
		index := ready[0]
		ready = ready[1:]
		order = append(order, index)
		for _, dependent := range dependents[index] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
				sort.Ints(ready)
			}
		}
	}
	if len(order) != len(nodes) {
		return nil, ErrInvalidGraph
	}
	levels := make([]int, len(nodes))
	for _, originalIndex := range order {
		for _, dependency := range dependenciesByIndex[originalIndex] {
			dependencyIndex := byOriginalID[dependency]
			levels[originalIndex] = max(levels[originalIndex], levels[dependencyIndex]+1)
		}
		normalized[originalIndex].level = levels[originalIndex]
	}
	ordered := make([]runtimeNode, 0, len(nodes))
	for _, originalIndex := range order {
		ordered = append(ordered, normalized[originalIndex])
	}
	return ordered, nil
}

func normalizeTimeouts(value Timeouts, defaults Timeouts) (Timeouts, error) {
	if value.Start < 0 || value.Ready < 0 || value.Quiesce < 0 || value.Stop < 0 {
		return Timeouts{}, ErrInvalidGraph
	}
	result := value
	if result.Start == 0 {
		result.Start = defaults.Start
	}
	if result.Ready == 0 {
		result.Ready = defaults.Ready
	}
	if result.Quiesce == 0 {
		result.Quiesce = defaults.Quiesce
	}
	if result.Stop == 0 {
		result.Stop = defaults.Stop
	}
	if result.Start <= 0 || result.Ready <= 0 || result.Quiesce <= 0 || result.Stop <= 0 {
		return Timeouts{}, ErrInvalidGraph
	}
	return result, nil
}

func runPhase(parent context.Context, timeout time.Duration, operation func(context.Context) error) error {
	if err := parent.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err := operation(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

func notifyObserver(ctx context.Context, observer Observer, event Event) {
	defer func() { _ = recover() }()
	observer.OnLifecycleEvent(ctx, event)
}

func isNilComponent(component Component) bool {
	if component == nil {
		return true
	}
	value := reflect.ValueOf(component)
	return value.Kind() == reflect.Pointer && value.IsNil()
}
