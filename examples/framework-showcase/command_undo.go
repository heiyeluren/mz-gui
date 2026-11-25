package main

import (
	"context"
	"strconv"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/commandbus"
	"github.com/heiyeluren/mz-gui/framework/go/interaction/commandundo"
	"github.com/heiyeluren/mz-gui/framework/go/interaction/inputmodel"
	"github.com/heiyeluren/mz-gui/framework/go/interaction/undoredo"
)

const showcaseNoticeCommand = "showcase.shell.set-notice"

type showcaseUndo struct {
	adapter *commandundo.Adapter
	stack   *undoredo.Stack
	notice  string
}

func newShowcaseUndo() (*showcaseUndo, error) {
	stack := undoredo.New(undoredo.Options{MaxDepth: 8})
	adapter, err := commandundo.New(commandundo.AdapterOptions{
		Bus:   commandbus.New(),
		Stack: stack,
	})
	if err != nil {
		return nil, err
	}
	session := &showcaseUndo{adapter: adapter, stack: stack}
	if err := adapter.Register(showcaseNoticeCommand, session.handleNotice); err != nil {
		return nil, err
	}
	_, err = adapter.Dispatch(context.Background(), commandbus.Command{
		Type:    showcaseNoticeCommand,
		Payload: "showcase-ready",
		Meta:    showcaseNoticeMeta("set-notice"),
	})
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (session *showcaseUndo) handleNotice(_ context.Context, command commandbus.Command) (commandbus.Result, undoredo.Action, error) {
	notice, _ := command.Payload.(string)
	previous := session.notice
	session.notice = notice
	return commandbus.Result{CommandType: command.Type, RequestID: command.Meta.RequestID}, undoredo.Action{
		ID: command.Meta.RequestID, Label: "设置示例公告", Meta: command.Meta, CanUndo: true,
		Undo: func(context.Context) error { session.notice = previous; return nil },
		Redo: func(context.Context) error { session.notice = notice; return nil },
	}, nil
}

func (session *showcaseUndo) recordAppliedNotice(notice string) error {
	if session == nil || session.stack == nil {
		return commandundo.ErrInvalidAdapter
	}
	previous := session.notice
	session.notice = notice
	meta := showcaseNoticeMeta("record-notice")
	if err := session.stack.Record(undoredo.Action{
		ID:      meta.RequestID,
		Label:   "记录已应用示例公告",
		Meta:    meta,
		CanUndo: true,
		Do:      func(context.Context) error { return nil },
		Undo:    func(context.Context) error { session.notice = previous; return nil },
		Redo:    func(context.Context) error { session.notice = notice; return nil },
	}); err != nil {
		session.notice = previous
		return err
	}
	return nil
}

func (session *showcaseUndo) Undo(ctx context.Context) error {
	if session == nil || session.adapter == nil {
		return commandundo.ErrInvalidAdapter
	}
	return session.adapter.Undo(ctx)
}

func (session *showcaseUndo) Redo(ctx context.Context) error {
	if session == nil || session.adapter == nil {
		return commandundo.ErrInvalidAdapter
	}
	return session.adapter.Redo(ctx)
}

func showcaseNoticeMeta(action string) inputmodel.RequestMeta {
	return inputmodel.RequestMeta{
		RequestID:     "showcase-notice-" + action + "-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		CorrelationID: "showcase-shell",
		WindowID:      "showcase",
		RouteID:       "showcase.shell",
		Target:        "notice",
		Source:        inputmodel.SourceAutomation,
		Device:        inputmodel.DeviceSystem,
		Timestamp:     time.Now(),
	}
}
