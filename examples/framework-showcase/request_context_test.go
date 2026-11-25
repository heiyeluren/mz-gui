package main

import (
	"context"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func TestBindShowcaseRequestWritesIdentityAndDeadline(t *testing.T) {
	now := time.Date(2026, 8, 21, 15, 20, 0, 0, time.UTC)
	ctx, cancel, err := bindShowcaseRequest(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	requestID, ok := model.RequestIDFromContext(ctx)
	if !ok || requestID.String() != showcaseRequestID {
		t.Fatalf("RequestID 未写入: %q ok=%v", requestID, ok)
	}
	caller, ok := model.CallerFromContext(ctx)
	if !ok || caller.Component != showcaseRequestCaller {
		t.Fatalf("Caller 未写入: %#v ok=%v", caller, ok)
	}
	meta, ok := model.RequestMetaFromContext(ctx)
	if !ok || meta.RequestID.String() != showcaseRequestID || meta.TraceID.String() != showcaseTraceID {
		t.Fatalf("RequestMeta 未写入: %#v ok=%v", meta, ok)
	}
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline || !deadline.Equal(now.Add(time.Minute)) {
		t.Fatalf("BindRequest 未应用 deadline: has=%v got=%v", hasDeadline, deadline)
	}
}

func TestBindShowcaseRequestRejectsNilContext(t *testing.T) {
	if _, _, err := bindShowcaseRequest(nil, time.Now().UTC()); err == nil {
		t.Fatal("空上下文必须拒绝")
	}
}
