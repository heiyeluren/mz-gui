package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeComponent struct {
	name     string
	events   *[]string
	startErr error
	readyErr error
	stopErr  error
}

func (component *fakeComponent) Name() string { return component.name }

func (component *fakeComponent) Start(context.Context) error {
	*component.events = append(*component.events, "start:"+component.name)
	return component.startErr
}

func (component *fakeComponent) Ready(context.Context) error {
	*component.events = append(*component.events, "ready:"+component.name)
	return component.readyErr
}

func (component *fakeComponent) Stop(context.Context) error {
	*component.events = append(*component.events, "stop:"+component.name)
	return component.stopErr
}

func TestSupervisorStartsInOrderAndStopsInReverse(t *testing.T) {
	events := make([]string, 0)
	supervisor, err := New([]Component{
		&fakeComponent{name: "transport", events: &events},
		&fakeComponent{name: "runtime", events: &events},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"start:transport", "ready:transport", "start:runtime", "ready:runtime", "stop:runtime", "stop:transport"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("生命周期顺序不正确:\nwant=%v\n got=%v", want, events)
	}
}

func TestSupervisorRollsBackStartedComponents(t *testing.T) {
	events := make([]string, 0)
	startFailure := errors.New("start failed")
	supervisor, err := New([]Component{
		&fakeComponent{name: "first", events: &events},
		&fakeComponent{name: "second", events: &events, startErr: startFailure},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); !errors.Is(err, startFailure) {
		t.Fatalf("应返回启动错误，实际为 %v", err)
	}
	want := []string{"start:first", "ready:first", "start:second", "stop:first"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("启动失败回滚顺序不正确: want=%v got=%v", want, events)
	}
}

func TestSupervisorRejectsInvalidComponentsAndIsIdempotent(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("空组件列表应被拒绝，实际为 %v", err)
	}
	events := make([]string, 0)
	if _, err := New([]Component{
		&fakeComponent{name: "same", events: &events},
		&fakeComponent{name: "same", events: &events},
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("重复组件名应被拒绝，实际为 %v", err)
	}
	supervisor, err := New([]Component{&fakeComponent{name: "one", events: &events}})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("重复 Start 应为幂等: %v", err)
	}
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatalf("重复 Stop 应为幂等: %v", err)
	}
	want := []string{"start:one", "ready:one", "stop:one"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("幂等调用产生额外事件: want=%v got=%v", want, events)
	}
}
