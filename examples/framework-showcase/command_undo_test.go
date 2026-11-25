package main

import (
	"context"
	"errors"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/undoredo"
)

func TestShowcaseUndoReversesShellNotice(t *testing.T) {
	session, err := newShowcaseUndo()
	if err != nil {
		t.Fatal(err)
	}
	if session.notice != "showcase-ready" {
		t.Fatalf("示例公告异常: %q", session.notice)
	}
	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if session.notice != "" {
		t.Fatalf("撤销后公告应为空: %q", session.notice)
	}
	if err := session.Redo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if session.notice != "showcase-ready" {
		t.Fatalf("重做后公告应恢复: %q", session.notice)
	}
	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.Undo(context.Background()); !errors.Is(err, undoredo.ErrEmptyStack) {
		t.Fatalf("空栈撤销错误语义异常: %v", err)
	}
}

func TestShowcaseRecordAppliedNoticeCanUndoAndRedo(t *testing.T) {
	session, err := newShowcaseUndo()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.recordAppliedNotice("showcase-restored"); err != nil {
		t.Fatal(err)
	}
	if session.notice != "showcase-restored" {
		t.Fatalf("已应用公告异常: %q", session.notice)
	}
	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if session.notice != "" {
		t.Fatalf("撤销已应用公告后应为空: %q", session.notice)
	}
	if err := session.Redo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if session.notice != "showcase-restored" {
		t.Fatalf("重做已应用公告应恢复: %q", session.notice)
	}
}
