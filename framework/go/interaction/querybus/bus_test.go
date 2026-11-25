package querybus

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
		RouteID:       "knowledge",
		Target:        "search-input",
		Source:        inputmodel.SourceKeyboard,
		Device:        inputmodel.DeviceKeyboard,
		Timestamp:     time.Now(),
	}
}

func TestAskRoutesQueryAndFillsResultMeta(t *testing.T) {
	bus := New()
	err := bus.Register("knowledge.search", func(ctx context.Context, query Query) (Result, error) {
		if query.Meta.Source != inputmodel.SourceKeyboard {
			t.Fatalf("来源未透传: %s", query.Meta.Source)
		}
		return Result{Value: []string{"a", "b"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := bus.Ask(context.Background(), Query{Type: "knowledge.search", Meta: validMeta()})
	if err != nil {
		t.Fatal(err)
	}
	if result.QueryType != "knowledge.search" || result.RequestID != "req-1" {
		t.Fatalf("查询结果元数据不完整: %#v", result)
	}
	items, ok := result.Value.([]string)
	if !ok || len(items) != 2 {
		t.Fatalf("查询结果值错误: %#v", result.Value)
	}
}

func TestAskValidatesRegistrationMetaAndContext(t *testing.T) {
	bus := New()
	if err := bus.Register("", func(context.Context, Query) (Result, error) { return Result{}, nil }); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("空查询类型应失败，实际为 %v", err)
	}
	if err := bus.Register("x", nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil handler 应失败，实际为 %v", err)
	}
	if err := bus.Register("x", func(context.Context, Query) (Result, error) { return Result{}, nil }); err != nil {
		t.Fatal(err)
	}
	if err := bus.Register("x", func(context.Context, Query) (Result, error) { return Result{}, nil }); !errors.Is(err, ErrHandlerExists) {
		t.Fatalf("重复注册应失败，实际为 %v", err)
	}
	if _, err := bus.Ask(context.Background(), Query{Type: "missing", Meta: validMeta()}); !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("未注册查询应失败，实际为 %v", err)
	}
	badMeta := validMeta()
	badMeta.Source = "invalid"
	if _, err := bus.Ask(context.Background(), Query{Type: "x", Meta: badMeta}); !errors.Is(err, inputmodel.ErrInvalidMeta) {
		t.Fatalf("无效 meta 应失败，实际为 %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bus.Ask(ctx, Query{Type: "x", Meta: validMeta()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消上下文应失败，实际为 %v", err)
	}
}
