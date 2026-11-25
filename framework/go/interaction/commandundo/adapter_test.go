package commandundo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/commandbus"
	"github.com/heiyeluren/mz-gui/framework/go/interaction/inputmodel"
	"github.com/heiyeluren/mz-gui/framework/go/interaction/undoredo"
)

func validMeta(requestID string) inputmodel.RequestMeta {
	return inputmodel.RequestMeta{
		RequestID:     requestID,
		CorrelationID: "corr-1",
		WindowID:      "win-1",
		RouteID:       "editor",
		Target:        "canvas",
		Source:        inputmodel.SourceKeyboard,
		Device:        inputmodel.DeviceKeyboard,
		Timestamp:     time.Now(),
	}
}

func TestAdapterDispatchUndoRedoReversibleCommand(t *testing.T) {
	bus := commandbus.New()
	stack := undoredo.New(undoredo.Options{MaxDepth: 8})
	adapter, err := New(AdapterOptions{Bus: bus, Stack: stack})
	if err != nil {
		t.Fatal(err)
	}
	value := 0
	if err := adapter.Register("editor.insert", func(ctx context.Context, command commandbus.Command) (commandbus.Result, undoredo.Action, error) {
		value++
		return commandbus.Result{Value: value}, undoredo.Action{
			ID:      command.Meta.RequestID,
			Label:   "插入",
			Meta:    command.Meta,
			CanUndo: true,
			Undo:    func(context.Context) error { value--; return nil },
			Redo:    func(context.Context) error { value++; return nil },
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Dispatch(context.Background(), commandbus.Command{Type: "editor.insert", Meta: validMeta("req-insert")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value != 1 || value != 1 {
		t.Fatalf("Dispatch 后状态异常: result=%#v value=%d", result, value)
	}
	if err := adapter.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if value != 0 {
		t.Fatalf("Undo 后值错误: %d", value)
	}
	if err := adapter.Redo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatalf("Redo 后值错误: %d", value)
	}
	snapshot := adapter.Snapshot()
	if !snapshot.CanUndo || snapshot.CanRedo || snapshot.NextUndoLabel != "插入" {
		t.Fatalf("快照异常: %#v", snapshot)
	}
}

func TestAdapterDoesNotRecordFailedOrIrreversibleCommands(t *testing.T) {
	bus := commandbus.New()
	stack := undoredo.New(undoredo.Options{})
	adapter, err := New(AdapterOptions{Bus: bus, Stack: stack})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Register("editor.fail", func(context.Context, commandbus.Command) (commandbus.Result, undoredo.Action, error) {
		return commandbus.Result{}, undoredo.Action{}, errors.New("handler failed")
	}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Register("editor.peek", func(context.Context, commandbus.Command) (commandbus.Result, undoredo.Action, error) {
		return commandbus.Result{Value: "ok"}, undoredo.Action{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Dispatch(context.Background(), commandbus.Command{Type: "editor.fail", Meta: validMeta("req-fail")}); err == nil {
		t.Fatal("失败命令应返回错误")
	}
	if _, err := adapter.Dispatch(context.Background(), commandbus.Command{Type: "editor.peek", Meta: validMeta("req-peek")}); err != nil {
		t.Fatal(err)
	}
	if snapshot := adapter.Snapshot(); snapshot.CanUndo {
		t.Fatalf("失败或不可逆命令不得入栈: %#v", snapshot)
	}
}

func TestAdapterRegistersUndoRedoCommands(t *testing.T) {
	bus := commandbus.New()
	stack := undoredo.New(undoredo.Options{})
	adapter, err := New(AdapterOptions{Bus: bus, Stack: stack})
	if err != nil {
		t.Fatal(err)
	}
	value := 0
	if err := adapter.Register("editor.insert", func(ctx context.Context, command commandbus.Command) (commandbus.Result, undoredo.Action, error) {
		value++
		return commandbus.Result{}, undoredo.Action{
			ID: command.Meta.RequestID, Label: "插入", Meta: command.Meta, CanUndo: true,
			Undo: func(context.Context) error { value--; return nil },
			Redo: func(context.Context) error { value++; return nil },
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RegisterStackCommands(); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Dispatch(context.Background(), commandbus.Command{Type: "editor.insert", Meta: validMeta("req-1")}); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Dispatch(context.Background(), commandbus.Command{Type: CommandUndo, Meta: validMeta("req-undo")}); err != nil {
		t.Fatal(err)
	}
	if value != 0 {
		t.Fatalf("interaction.undo 未生效: %d", value)
	}
	if _, err := bus.Dispatch(context.Background(), commandbus.Command{Type: CommandRedo, Meta: validMeta("req-redo")}); err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatalf("interaction.redo 未生效: %d", value)
	}
}

func TestNewAdapterRejectsMissingDependencies(t *testing.T) {
	if _, err := New(AdapterOptions{}); !errors.Is(err, ErrInvalidAdapter) {
		t.Fatalf("缺少依赖必须拒绝: %v", err)
	}
}
