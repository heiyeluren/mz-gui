package commandbus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/inputmodel"
)

func validMeta() inputmodel.RequestMeta {
	return inputmodel.RequestMeta{
		RequestID:      "req-1",
		CorrelationID:  "corr-1",
		WindowID:       "win-1",
		RouteID:        "chat",
		Target:         "send-button",
		Source:         inputmodel.SourceMouse,
		Device:         inputmodel.DeviceMouse,
		Timestamp:      time.Now(),
		IdempotencyKey: "idem-1",
	}
}

func TestDispatchRoutesCommandAndFillsResultMeta(t *testing.T) {
	bus := New()
	err := bus.Register("chat.send", func(ctx context.Context, command Command) (Result, error) {
		if command.Meta.Source != inputmodel.SourceMouse {
			t.Fatalf("来源未透传: %s", command.Meta.Source)
		}
		return Result{Value: "ok"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := bus.Dispatch(context.Background(), Command{Type: "chat.send", Meta: validMeta()})
	if err != nil {
		t.Fatal(err)
	}
	if result.CommandType != "chat.send" || result.RequestID != "req-1" || result.Value != "ok" {
		t.Fatalf("命令结果元数据不完整: %#v", result)
	}
}

func TestDispatchRejectsDuplicatePendingCommand(t *testing.T) {
	bus := New()
	block := make(chan struct{})
	release := make(chan struct{})
	err := bus.Register("chat.send", func(ctx context.Context, command Command) (Result, error) {
		close(block)
		<-release
		return Result{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	meta := validMeta()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = bus.Dispatch(context.Background(), Command{Type: "chat.send", Meta: meta})
	}()
	<-block
	if _, err := bus.Dispatch(context.Background(), Command{Type: "chat.send", Meta: meta}); !errors.Is(err, ErrDuplicatePending) {
		t.Fatalf("重复 pending 应被拒绝，实际为 %v", err)
	}
	close(release)
	wg.Wait()
}

func TestDispatchValidatesRegistrationMetaAndContext(t *testing.T) {
	bus := New()
	if err := bus.Register("", func(context.Context, Command) (Result, error) { return Result{}, nil }); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("空命令类型应失败，实际为 %v", err)
	}
	if err := bus.Register("x", nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil handler 应失败，实际为 %v", err)
	}
	if err := bus.Register("x", func(context.Context, Command) (Result, error) { return Result{}, nil }); err != nil {
		t.Fatal(err)
	}
	if err := bus.Register("x", func(context.Context, Command) (Result, error) { return Result{}, nil }); !errors.Is(err, ErrHandlerExists) {
		t.Fatalf("重复注册应失败，实际为 %v", err)
	}
	if _, err := bus.Dispatch(context.Background(), Command{Type: "missing", Meta: validMeta()}); !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("未注册命令应失败，实际为 %v", err)
	}
	badMeta := validMeta()
	badMeta.RequestID = ""
	if _, err := bus.Dispatch(context.Background(), Command{Type: "x", Meta: badMeta}); !errors.Is(err, inputmodel.ErrInvalidMeta) {
		t.Fatalf("无效 meta 应失败，实际为 %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bus.Dispatch(ctx, Command{Type: "x", Meta: validMeta()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消上下文应失败，实际为 %v", err)
	}
}
