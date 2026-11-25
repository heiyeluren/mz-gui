package main

import (
	"context"
	"fmt"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

const (
	showcaseRequestID     = "showcase-request"
	showcaseTraceID       = "showcase-trace"
	showcaseRequestCaller = "framework.showcase"
	showcaseInstanceID    = "showcase-1"
)

func showcaseRequestMeta(now time.Time) (model.RequestMeta, error) {
	now = now.UTC()
	version, err := model.ParseVersion("1.0.0")
	if err != nil {
		return model.RequestMeta{}, err
	}
	deadline, err := model.NewTimestamp(now.Add(time.Minute))
	if err != nil {
		return model.RequestMeta{}, err
	}
	caller := model.Caller{Component: showcaseRequestCaller, Version: version, InstanceID: showcaseInstanceID}
	return model.RequestMeta{
		RequestID: showcaseRequestID, TraceID: showcaseTraceID, Deadline: deadline, Caller: caller,
	}, nil
}

func bindShowcaseRequest(ctx context.Context, now time.Time) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return context.Background(), func() {}, fmt.Errorf("showcase 请求上下文无效")
	}
	now = now.UTC()
	meta, err := showcaseRequestMeta(now)
	if err != nil {
		return ctx, func() {}, err
	}
	ctx = model.WithRequestID(ctx, showcaseRequestID)
	ctx = model.WithCaller(ctx, meta.Caller)
	ctx = model.WithRequestMeta(ctx, meta)
	return model.BindRequest(ctx, now, meta)
}
