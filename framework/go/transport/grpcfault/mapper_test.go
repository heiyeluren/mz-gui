package grpcfault

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
	"github.com/heiyeluren/mz-gui/framework/go/core/panicguard"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCFaultRoundTripPreservesSafeContract(t *testing.T) {
	failure, err := fault.New(fault.Spec{
		Code: fault.CodeUnavailable, ReasonCode: "network.remote_unavailable", MessageKey: "errors.remote_unavailable",
		UserMessage: "远程服务暂时不可用", Retryable: true, RetryAfter: 2 * time.Second,
		RequestID: "request-1", TraceID: "trace-1", SafeDetails: map[string]string{"service": "remote"},
		FieldIssues: []fault.FieldIssue{{Path: "endpoint", Rule: "reachable", MessageKey: "errors.endpoint_unreachable"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mapped := ToStatus(failure)
	if status.Code(mapped) != codes.Unavailable || status.Convert(mapped).Message() != "远程服务暂时不可用" {
		t.Fatalf("gRPC 映射异常: %v", mapped)
	}
	roundTrip := FromStatus(mapped)
	public := fault.Public(roundTrip)
	if public.ReasonCode != "network.remote_unavailable" || public.RequestID != "request-1" || public.RetryAfterMS != 2000 || public.SafeDetails["service"] != "remote" || len(public.FieldIssues) != 1 {
		t.Fatalf("gRPC 往返丢失安全字段: %#v", public)
	}
}

func TestUnaryServerBoundaryContainsPanicAndPreservesExistingStatus(t *testing.T) {
	interceptor := UnaryServerBoundary(panicguard.Options{Boundary: "grpc"})
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/sample.v1.Service/Read"}, func(context.Context, any) (any, error) {
		panic("secret panic")
	})
	if status.Code(err) != codes.Internal || strings.Contains(err.Error(), "secret") {
		t.Fatalf("gRPC panic 边界异常: %v", err)
	}
	want := status.Error(codes.InvalidArgument, "参数无效")
	_, err = interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/sample.v1.Service/Read"}, func(context.Context, any) (any, error) {
		return nil, want
	})
	if status.Code(err) != codes.InvalidArgument || status.Convert(err).Message() != "参数无效" {
		t.Fatalf("已有 gRPC status 不得被重复翻译: %v", err)
	}
}

func TestStreamServerBoundaryContainsPanicAndMapsFault(t *testing.T) {
	interceptor := StreamServerBoundary(panicguard.Options{Boundary: "grpc-stream"})
	stream := &stubServerStream{ctx: context.Background()}
	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/sample.v1.Service/Watch"}, func(any, grpc.ServerStream) error {
		panic("secret stream panic")
	})
	if status.Code(err) != codes.Internal || strings.Contains(err.Error(), "secret") {
		t.Fatalf("gRPC stream panic 边界异常: %v", err)
	}

	failure, createErr := fault.New(fault.Spec{
		Code: fault.CodeUnavailable, ReasonCode: "network.remote_unavailable",
		MessageKey: "errors.remote_unavailable", UserMessage: "远程服务暂时不可用", Retryable: true,
	})
	if createErr != nil {
		t.Fatal(createErr)
	}
	err = interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/sample.v1.Service/Watch"}, func(any, grpc.ServerStream) error {
		return failure
	})
	if status.Code(err) != codes.Unavailable || status.Convert(err).Message() != "远程服务暂时不可用" {
		t.Fatalf("gRPC stream 错误映射异常: %v", err)
	}
}

func TestGRPCFaultSanitizesUnknownErrors(t *testing.T) {
	mapped := ToStatus(errors.New("token=secret /Users/private/file"))
	if status.Code(mapped) != codes.Internal || strings.Contains(mapped.Error(), "secret") || strings.Contains(mapped.Error(), "/Users/") {
		t.Fatalf("未知错误映射泄露内部信息: %v", mapped)
	}
}

func TestGRPCFaultRoundTripPreservesEveryFrameworkCode(t *testing.T) {
	for _, code := range fault.AllCodes() {
		failure, err := fault.New(fault.Spec{
			Code: code, ReasonCode: "test.round_trip", MessageKey: "errors.round_trip", UserMessage: "测试错误",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := FromStatus(ToStatus(failure)).Code(); got != code {
			t.Errorf("错误分类 %s 往返后变为 %s", code, got)
		}
	}
}

type stubServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *stubServerStream) Context() context.Context {
	if stream == nil || stream.ctx == nil {
		return context.Background()
	}
	return stream.ctx
}
