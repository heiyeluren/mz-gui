package lifecycle

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

func TestGraphSupervisorUsesStableTopologyAndOptionalDegradation(t *testing.T) {
	operations := make([]string, 0)
	observer := &recordingObserver{}
	supervisor, err := NewGraph([]Node{
		{Component: graphComponent("application", &operations), Requires: []string{"transport"}, OptionalRequires: []string{"metrics"}},
		{Component: graphComponent("metrics", &operations, withStartError(errors.New("metrics unavailable"))), FailurePolicy: FailureOptional},
		{Component: graphComponent("transport", &operations)},
	}, Options{Clock: fixedLifecycleClock{now: time.Date(2026, 8, 15, 2, 0, 0, 0, time.UTC)}, Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("可选节点失败不应阻断应用: %v", err)
	}
	want := []string{"start:metrics", "start:transport", "ready:transport", "start:application", "ready:application"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("拓扑启动顺序异常: want=%v got=%v", want, operations)
	}
	snapshot := supervisor.Snapshot()
	if !snapshot.Running || !snapshot.Degraded || stateOf(t, snapshot, "metrics") != StateDegraded || stateOf(t, snapshot, "application") != StateReady {
		t.Fatalf("可选降级快照异常: %#v", snapshot)
	}
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want = append(want, "quiesce:application", "quiesce:transport", "stop:application", "stop:transport")
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("反向排空/停止顺序异常: want=%v got=%v", want, operations)
	}
	observer.assertOrdered(t)
}

func TestGraphSupervisorStartsIndependentNodesInBoundedParallelWaves(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	active := 0
	maxActive := 0
	applicationOperations := make([]string, 0)
	telemetryOperations := make([]string, 0)
	transportOperations := make([]string, 0)
	parallelComponent := func(name string, operations *[]string) *graphTestComponent {
		return graphComponent(name, operations, withStart(func(ctx context.Context) error {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			started <- name
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			mu.Lock()
			active--
			mu.Unlock()
			return nil
		}))
	}
	supervisor, err := NewGraph([]Node{
		{Component: graphComponent("application", &applicationOperations), Requires: []string{"transport", "telemetry"}},
		{Component: parallelComponent("telemetry", &telemetryOperations)},
		{Component: parallelComponent("transport", &transportOperations)},
	}, Options{MaxParallelStarts: 2})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- supervisor.Start(context.Background()) }()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatal("独立节点未在同一依赖层内并行启动")
		}
	}
	if seen["application"] {
		t.Fatal("后继节点不得早于依赖层全部就绪")
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	observedMax := maxActive
	mu.Unlock()
	if observedMax != 2 {
		t.Fatalf("并行启动上限未生效: max=%d", observedMax)
	}
	if stateOf(t, supervisor.Snapshot(), "application") != StateReady {
		t.Fatalf("后继节点未在依赖层完成后启动: %#v", supervisor.Snapshot())
	}
}

func TestGraphSupervisorRollsBackSuccessfulPeerWhenParallelRequiredNodeFails(t *testing.T) {
	failed := errors.New("sidecar failed")
	peerStarted := make(chan struct{})
	releaseFailure := make(chan struct{})
	peerOperations := make([]string, 0)
	failingOperations := make([]string, 0)
	peer := graphComponent("peer", &peerOperations, withStart(func(context.Context) error {
		close(peerStarted)
		return nil
	}))
	failing := graphComponent("failing", &failingOperations, withStart(func(context.Context) error {
		<-peerStarted
		<-releaseFailure
		return failed
	}))
	supervisor, err := NewGraph([]Node{{Component: peer}, {Component: failing}}, Options{MaxParallelStarts: 2})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- supervisor.Start(context.Background()) }()
	<-peerStarted
	close(releaseFailure)
	if err := <-result; !errors.Is(err, failed) {
		t.Fatalf("并行必需节点失败应返回原始错误: %v", err)
	}
	foundStop := false
	for _, operation := range peerOperations {
		foundStop = foundStop || operation == "stop:peer"
	}
	if !foundStop {
		t.Fatalf("同层已启动节点必须回滚: %v", peerOperations)
	}
}

func TestGraphSupervisorRejectsInvalidParallelStartLimit(t *testing.T) {
	operations := make([]string, 0)
	if _, err := NewGraph([]Node{{Component: graphComponent("runtime", &operations)}}, Options{MaxParallelStarts: -1}); !errors.Is(err, ErrInvalidGraph) {
		t.Fatalf("负并行度必须被拒绝: %v", err)
	}
}

func TestGraphSupervisorDoesNotStartQueuedPeerAfterRequiredFailure(t *testing.T) {
	failed := errors.New("first failed")
	firstOperations := make([]string, 0)
	queuedOperations := make([]string, 0)
	supervisor, err := NewGraph([]Node{
		{Component: graphComponent("first", &firstOperations, withStartError(failed))},
		{Component: graphComponent("queued", &queuedOperations)},
	}, Options{MaxParallelStarts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); !errors.Is(err, failed) {
		t.Fatalf("首个必需节点失败应原样返回: %v", err)
	}
	if len(queuedOperations) != 0 || stateOf(t, supervisor.Snapshot(), "queued") != StateCreated {
		t.Fatalf("取消后尚未调度的节点必须保持 created: operations=%v snapshot=%#v", queuedOperations, supervisor.Snapshot())
	}
}

func TestGraphSupervisorRejectsMissingDependenciesAndCycles(t *testing.T) {
	operations := make([]string, 0)
	tests := []struct {
		name  string
		nodes []Node
	}{
		{name: "缺少必需依赖", nodes: []Node{{Component: graphComponent("app", &operations), Requires: []string{"missing"}}}},
		{name: "缺少可选依赖声明", nodes: []Node{{Component: graphComponent("app", &operations), OptionalRequires: []string{"missing"}}}},
		{name: "依赖环", nodes: []Node{
			{Component: graphComponent("first", &operations), Requires: []string{"second"}},
			{Component: graphComponent("second", &operations), Requires: []string{"first"}},
		}},
		{name: "重复依赖", nodes: []Node{
			{Component: graphComponent("base", &operations)},
			{Component: graphComponent("app", &operations), Requires: []string{"base", "base"}},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewGraph(test.nodes, Options{}); !errors.Is(err, ErrInvalidGraph) {
				t.Fatalf("非法 DAG 应被拒绝: %v", err)
			}
		})
	}
}

func TestBuiltinLifecycleContractMatchesPublicStateAndReasonConstants(t *testing.T) {
	contract, err := BuiltinContract()
	if err != nil {
		t.Fatal(err)
	}
	if len(contract.States) != 9 || len(contract.Reasons) != 11 {
		t.Fatalf("生命周期契约条目数量异常: states=%d reasons=%d", len(contract.States), len(contract.Reasons))
	}
	for _, state := range []State{
		StateCreated, StateStarting, StateReady, StateDegraded, StateFailed, StateCrashLoop, StateQuiescing, StateStopping, StateStopped,
	} {
		if !contract.HasState(state) {
			t.Errorf("生命周期契约缺少状态 %s", state)
		}
	}
	checks := []struct {
		reason string
		state  State
	}{
		{ReasonCreated, StateCreated},
		{ReasonStarting, StateStarting},
		{ReasonReady, StateReady},
		{ReasonStartFailed, StateFailed},
		{ReasonStartFailed, StateDegraded},
		{ReasonReadyFailed, StateFailed},
		{ReasonDependencyUnavailable, StateDegraded},
		{ReasonQuiescing, StateQuiescing},
		{ReasonQuiesceFailed, StateDegraded},
		{ReasonStopping, StateStopping},
		{ReasonStopped, StateStopped},
		{ReasonStopFailed, StateFailed},
	}
	for _, check := range checks {
		if !contract.Allows(check.reason, check.state) {
			t.Errorf("生命周期契约未登记 %s -> %s", check.reason, check.state)
		}
	}
	data, err := os.ReadFile("v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeContract(data); err != nil {
		t.Fatalf("仓库生命周期契约无效: %v", err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"schema_version":1,"release":"1.0.0","unexpected":true,"states":["created"],"reasons":[]}`),
		[]byte(`{"schema_version":1,"release":"1.0.0","states":["created","created"],"reasons":[{"reason_code":"lifecycle.created","target_states":["created"]}]}`),
		[]byte(`{"schema_version":1,"release":"1.0.0","states":["created"],"reasons":[{"reason_code":"lifecycle.created","target_states":["missing"]}]}`),
	} {
		if _, err := DecodeContract(invalid); !errors.Is(err, ErrInvalidContract) {
			t.Errorf("非法生命周期契约应被拒绝: %v", err)
		}
	}
}

func TestGraphSupervisorRollsBackWithBoundedUncancelledContext(t *testing.T) {
	operations := make([]string, 0)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	startFailure := errors.New("runtime failed")
	cleanupContextWasActive := false
	base := graphComponent("base", &operations, withStop(func(ctx context.Context) error {
		cleanupContextWasActive = ctx.Err() == nil
		return nil
	}))
	runtime := graphComponent("runtime", &operations, withStart(func(context.Context) error {
		cancel()
		return startFailure
	}))
	supervisor, err := NewGraph([]Node{
		{Component: base},
		{Component: runtime, Requires: []string{"base"}},
	}, Options{DefaultTimeouts: Timeouts{Start: time.Second, Ready: time.Second, Quiesce: time.Second, Stop: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(parent); !errors.Is(err, startFailure) {
		t.Fatalf("应返回必需节点错误: %v", err)
	}
	if !cleanupContextWasActive {
		t.Fatal("父上下文取消后，已启动节点仍必须获得有期限的有效清理上下文")
	}
	want := []string{"start:base", "ready:base", "start:runtime", "stop:base"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("失败回滚异常: want=%v got=%v", want, operations)
	}
	if supervisor.Snapshot().Running {
		t.Fatal("回滚后不得保持 running")
	}
}

func TestGraphSupervisorAppliesReadyDeadlineAndStopsStartedNode(t *testing.T) {
	operations := make([]string, 0)
	component := graphComponent("slow", &operations, withReady(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}))
	supervisor, err := NewGraph([]Node{{Component: component, Timeouts: Timeouts{Ready: 10 * time.Millisecond}}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Ready 超时应明确返回 deadline: %v", err)
	}
	want := []string{"start:slow", "ready:slow", "stop:slow"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("Ready 超时回滚异常: want=%v got=%v", want, operations)
	}
}

func TestGraphSupervisorDoesNotSwallowParentCancellationForOptionalNode(t *testing.T) {
	operations := make([]string, 0)
	parent, cancel := context.WithCancel(context.Background())
	component := graphComponent("optional", &operations, withStart(func(context.Context) error {
		cancel()
		return context.Canceled
	}))
	supervisor, err := NewGraph([]Node{{Component: component, FailurePolicy: FailureOptional}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(parent); !errors.Is(err, context.Canceled) {
		t.Fatalf("父上下文取消不得降级后继续: %v", err)
	}
	if supervisor.Snapshot().Running {
		t.Fatal("父上下文取消后不得标记 running")
	}
}

func TestGraphSupervisorPublishesCancellationEventWithActiveObserverContext(t *testing.T) {
	type contextKey string
	const traceKey contextKey = "trace"
	operations := make([]string, 0)
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), traceKey, "trace-52"))
	var observerContextErr error
	var observerTrace string
	component := graphComponent("runtime", &operations, withStart(func(context.Context) error {
		cancel()
		return context.Canceled
	}))
	supervisor, err := NewGraph([]Node{{Component: component}}, Options{Observer: ObserverFunc(func(ctx context.Context, event Event) {
		if event.ReasonCode == ReasonStartFailed {
			observerContextErr = ctx.Err()
			observerTrace, _ = ctx.Value(traceKey).(string)
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(parent); !errors.Is(err, context.Canceled) {
		t.Fatalf("启动应返回父上下文取消: %v", err)
	}
	if observerContextErr != nil || observerTrace != "trace-52" {
		t.Fatalf("取消事件必须保留上下文值且不继承取消状态: err=%v trace=%q", observerContextErr, observerTrace)
	}
}

func TestGraphSupervisorStopCompletesBoundedCleanupAfterCancellation(t *testing.T) {
	type contextKey string
	const shutdownKey contextKey = "shutdown"
	operations := make([]string, 0)
	cleanupCalls := 0
	cleanupContextValid := true
	checkCleanupContext := func(ctx context.Context) error {
		cleanupCalls++
		_, hasDeadline := ctx.Deadline()
		if ctx.Err() != nil || !hasDeadline || ctx.Value(shutdownKey) != "shutdown-52" {
			cleanupContextValid = false
		}
		return nil
	}
	component := graphComponent("runtime", &operations,
		withQuiesce(checkCleanupContext),
		withStop(checkCleanupContext),
	)
	supervisor, err := NewGraph([]Node{{Component: component}}, Options{DefaultTimeouts: Timeouts{
		Start: time.Second, Ready: time.Second, Quiesce: time.Second, Stop: time.Second,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), shutdownKey, "shutdown-52"))
	cancel()
	if err := supervisor.Stop(parent); !errors.Is(err, context.Canceled) {
		t.Fatalf("清理完成后仍应报告调用方取消: %v", err)
	}
	if cleanupCalls != 2 || !cleanupContextValid {
		t.Fatalf("取消后必须以保留值且有期限的上下文完成排空和停止: calls=%d valid=%t", cleanupCalls, cleanupContextValid)
	}
	snapshot := supervisor.Snapshot()
	if !snapshot.Closed || snapshot.Running || stateOf(t, snapshot, "runtime") != StateStopped {
		t.Fatalf("取消后的清理状态异常: %#v", snapshot)
	}
}

func TestGraphSupervisorBlocksRelatedNodeWhenOptionalDependencyIsUnavailable(t *testing.T) {
	operations := make([]string, 0)
	supervisor, err := NewGraph([]Node{
		{Component: graphComponent("storage", &operations, withStartError(errors.New("storage unavailable"))), FailurePolicy: FailureOptional},
		{Component: graphComponent("application", &operations), Requires: []string{"storage"}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("依赖可选节点的必需节点必须明确失败: %v", err)
	}
	if stateOf(t, supervisor.Snapshot(), "application") != StateFailed {
		t.Fatalf("关联节点未进入 failed: %#v", supervisor.Snapshot())
	}
}

func TestGraphSupervisorFreezesComponentIDAndContainsObserverPanic(t *testing.T) {
	operations := make([]string, 0)
	component := graphComponent("stable", &operations)
	supervisor, err := NewGraph([]Node{{Component: component}}, Options{Observer: ObserverFunc(func(context.Context, Event) {
		panic("observer failed")
	})})
	if err != nil {
		t.Fatal(err)
	}
	component.name = "changed-after-construction"
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stateOf(t, supervisor.Snapshot(), "stable") != StateReady {
		t.Fatalf("组件 ID 未在构造时冻结: %#v", supervisor.Snapshot())
	}
}

func TestGraphSupervisorLegacyConstructorAndConcurrentSnapshots(t *testing.T) {
	operations := make([]string, 0)
	supervisor, err := New([]Component{
		graphComponent("transport", &operations),
		graphComponent("runtime", &operations),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for count := 0; count < 100; count++ {
				if len(supervisor.Snapshot().Components) != 2 {
					t.Error("并发快照丢失组件")
					return
				}
			}
		}()
	}
	wait.Wait()
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"start:transport", "ready:transport", "start:runtime", "ready:runtime", "quiesce:runtime", "quiesce:transport", "stop:runtime", "stop:transport"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("兼容构造器未保持输入依赖顺序: want=%v got=%v", want, operations)
	}
}

type graphComponentOption func(*graphTestComponent)

type graphTestComponent struct {
	name       string
	operations *[]string
	start      func(context.Context) error
	ready      func(context.Context) error
	quiesce    func(context.Context) error
	stop       func(context.Context) error
}

func graphComponent(name string, operations *[]string, options ...graphComponentOption) *graphTestComponent {
	component := &graphTestComponent{name: name, operations: operations}
	for _, option := range options {
		option(component)
	}
	return component
}

func withStart(operation func(context.Context) error) graphComponentOption {
	return func(component *graphTestComponent) { component.start = operation }
}

func withStartError(err error) graphComponentOption {
	return withStart(func(context.Context) error { return err })
}

func withReady(operation func(context.Context) error) graphComponentOption {
	return func(component *graphTestComponent) { component.ready = operation }
}

func withQuiesce(operation func(context.Context) error) graphComponentOption {
	return func(component *graphTestComponent) { component.quiesce = operation }
}

func withStop(operation func(context.Context) error) graphComponentOption {
	return func(component *graphTestComponent) { component.stop = operation }
}

func (component *graphTestComponent) Name() string { return component.name }

func (component *graphTestComponent) Start(ctx context.Context) error {
	*component.operations = append(*component.operations, "start:"+component.name)
	if component.start != nil {
		return component.start(ctx)
	}
	return nil
}

func (component *graphTestComponent) Ready(ctx context.Context) error {
	*component.operations = append(*component.operations, "ready:"+component.name)
	if component.ready != nil {
		return component.ready(ctx)
	}
	return nil
}

func (component *graphTestComponent) Quiesce(ctx context.Context) error {
	*component.operations = append(*component.operations, "quiesce:"+component.name)
	if component.quiesce != nil {
		return component.quiesce(ctx)
	}
	return nil
}

func (component *graphTestComponent) Stop(ctx context.Context) error {
	*component.operations = append(*component.operations, "stop:"+component.name)
	if component.stop != nil {
		return component.stop(ctx)
	}
	return nil
}

type recordingObserver struct {
	mu     sync.Mutex
	events []Event
}

func (observer *recordingObserver) OnLifecycleEvent(_ context.Context, event Event) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.events = append(observer.events, event)
}

func (observer *recordingObserver) assertOrdered(t *testing.T) {
	t.Helper()
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.events) == 0 {
		t.Fatal("生命周期事件为空")
	}
	for index, event := range observer.events {
		if event.Sequence != uint64(index+1) || event.ComponentID == "" || event.ReasonCode == "" || event.ObservedAt.Time().IsZero() {
			t.Fatalf("生命周期事件不稳定: index=%d event=%#v", index, event)
		}
		if index > 0 && event.ObservedAt.Time().Before(observer.events[index-1].ObservedAt.Time()) {
			t.Fatalf("生命周期事件时间倒退: previous=%#v current=%#v", observer.events[index-1], event)
		}
	}
}

type fixedLifecycleClock struct{ now time.Time }

func (clock fixedLifecycleClock) Now() time.Time                       { return clock.now }
func (clock fixedLifecycleClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }
func (clock fixedLifecycleClock) NewTicker(time.Duration) (frameworkclock.Ticker, error) {
	return nil, errors.New("测试不支持 ticker")
}

func stateOf(t *testing.T, snapshot Snapshot, componentID string) State {
	t.Helper()
	for _, component := range snapshot.Components {
		if component.ComponentID.String() == componentID {
			return component.State
		}
	}
	t.Fatalf("快照缺少组件 %s", componentID)
	return ""
}
