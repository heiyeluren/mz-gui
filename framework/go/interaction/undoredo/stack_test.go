package undoredo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/inputmodel"
)

func validMeta() inputmodel.RequestMeta {
	return inputmodel.RequestMeta{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		WindowID:      "win-1",
		RouteID:       "chat",
		Target:        "editor",
		Source:        inputmodel.SourceKeyboard,
		Device:        inputmodel.DeviceKeyboard,
		Timestamp:     time.Now(),
	}
}

func TestStackExecuteUndoRedo(t *testing.T) {
	stack := New(Options{MaxDepth: 3})
	value := 0
	action := Action{
		ID:      "insert-text",
		Label:   "插入文本",
		Meta:    validMeta(),
		CanUndo: true,
		Do: func(context.Context) error {
			value++
			return nil
		},
		Undo: func(context.Context) error {
			value--
			return nil
		},
	}
	if err := stack.Execute(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatalf("执行后值错误: %d", value)
	}
	if err := stack.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if value != 0 {
		t.Fatalf("撤销后值错误: %d", value)
	}
	if err := stack.Redo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatalf("重做后值错误: %d", value)
	}
	snapshot := stack.Snapshot()
	if !snapshot.CanUndo || snapshot.CanRedo || snapshot.NextUndoID != "insert-text" || snapshot.NextUndoLabel != "插入文本" {
		t.Fatalf("快照错误: %#v", snapshot)
	}
	if got := snapshot.Speak(); got != "撤销，插入文本" {
		t.Fatalf("撤销读法 = %s", got)
	}
	if got := snapshot.SpeakRedo(); got != "无法重做" {
		t.Fatalf("无可重做读法 = %s", got)
	}
}

func TestSnapshotSpeakRedo(t *testing.T) {
	stack := New(Options{MaxDepth: 3})
	if err := stack.Execute(context.Background(), reversibleAction("insert-text")); err != nil {
		t.Fatal(err)
	}
	if err := stack.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := stack.Snapshot()
	if got := snapshot.SpeakRedo(); got != "重做，insert-text" {
		t.Fatalf("重做读法 = %s", got)
	}
}

func TestStackClearsRedoAfterNewExecute(t *testing.T) {
	stack := New(Options{MaxDepth: 3})
	action := reversibleAction("a")
	if err := stack.Execute(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	if err := stack.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot := stack.Snapshot(); !snapshot.CanRedo {
		t.Fatalf("撤销后应可重做: %#v", snapshot)
	}
	if err := stack.Execute(context.Background(), reversibleAction("b")); err != nil {
		t.Fatal(err)
	}
	if snapshot := stack.Snapshot(); snapshot.CanRedo {
		t.Fatalf("新动作应清空 redo 栈: %#v", snapshot)
	}
}

func TestStackRejectsInvalidAndEmptyOperations(t *testing.T) {
	stack := New(Options{})
	if err := stack.Undo(context.Background()); !errors.Is(err, ErrEmptyStack) {
		t.Fatalf("空栈撤销应失败，实际为 %v", err)
	}
	if err := stack.Redo(context.Background()); !errors.Is(err, ErrEmptyStack) {
		t.Fatalf("空栈重做应失败，实际为 %v", err)
	}
	action := reversibleAction("x")
	action.CanUndo = false
	if err := stack.Execute(context.Background(), action); !errors.Is(err, ErrNotReversible) {
		t.Fatalf("不可逆动作应失败，实际为 %v", err)
	}
	action = reversibleAction("x")
	action.ID = ""
	if err := stack.Execute(context.Background(), action); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("无 ID 动作应失败，实际为 %v", err)
	}
}

func TestStackRecordDoesNotReplayDo(t *testing.T) {
	stack := New(Options{MaxDepth: 3})
	value := 1
	did := 0
	action := Action{
		ID:      "already-done",
		Label:   "已执行",
		Meta:    validMeta(),
		CanUndo: true,
		Do: func(context.Context) error {
			did++
			return nil
		},
		Undo: func(context.Context) error {
			value--
			return nil
		},
	}
	if err := stack.Record(action); err != nil {
		t.Fatal(err)
	}
	if did != 0 || value != 1 {
		t.Fatalf("Record 不得再次执行 Do: did=%d value=%d", did, value)
	}
	if err := stack.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if value != 0 || did != 0 {
		t.Fatalf("Undo 应只补偿: did=%d value=%d", did, value)
	}
}

func TestStackTrimsToMaxDepth(t *testing.T) {
	stack := New(Options{MaxDepth: 2})
	for _, id := range []string{"a", "b", "c"} {
		if err := stack.Execute(context.Background(), reversibleAction(id)); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := stack.Snapshot()
	if snapshot.UndoDepth != 2 || snapshot.NextUndoID != "c" {
		t.Fatalf("最大深度裁剪错误: %#v", snapshot)
	}
	if err := stack.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := stack.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := stack.Undo(context.Background()); !errors.Is(err, ErrEmptyStack) {
		t.Fatalf("第三次撤销应为空，实际为 %v", err)
	}
}

func reversibleAction(id string) Action {
	return Action{
		ID:      id,
		Label:   id,
		Meta:    validMeta(),
		CanUndo: true,
		Do:      func(context.Context) error { return nil },
		Undo:    func(context.Context) error { return nil },
	}
}
