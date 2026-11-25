package fault

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestMapperProducesRegisteredSentinelAndKeepsCause(t *testing.T) {
	mapper := NewMapper()
	if err := mapper.Bind(os.ErrNotExist, Spec{
		Code: CodeNotFound, ReasonCode: "core.resource_missing",
		MessageKey: "errors.not_found", UserMessage: "资源不存在",
	}); err != nil {
		t.Fatal(err)
	}
	wrapped := errors.Join(os.ErrNotExist, errors.New("token=secret"))
	failure := mapper.Produce(wrapped)
	if failure == nil || failure.Code() != CodeNotFound || failure.ReasonCode() != "core.resource_missing" {
		t.Fatalf("映射结果错误: %#v", failure)
	}
	if !errors.Is(failure, os.ErrNotExist) {
		t.Fatal("必须保留 sentinel cause")
	}
	if strings.Contains(Public(failure).UserMessage, "secret") {
		t.Fatal("映射结果不得泄露内部 cause")
	}
}

func TestMapperFallsBackToNormalizeAndRejectsDuplicates(t *testing.T) {
	mapper := NewMapper()
	if err := mapper.Bind(io.ErrClosedPipe, Spec{
		Code: CodeUnavailable, ReasonCode: "core.stream_closed",
		MessageKey: "errors.unavailable", UserMessage: "通道已关闭", Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mapper.Bind(io.ErrClosedPipe, Spec{
		Code: CodeUnavailable, ReasonCode: "core.stream_closed",
		MessageKey: "errors.unavailable", UserMessage: "通道已关闭", Retryable: true,
	}); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("重复绑定错误 = %v", err)
	}
	failure := mapper.Produce(errors.New("unknown boom"))
	if failure.Code() != CodeInternal || failure.ReasonCode() != "core.unknown_internal" {
		t.Fatalf("未登记错误应回退 Normalize: %#v", failure)
	}
	if mapper.Produce(nil) != nil {
		t.Fatal("nil 不得映射")
	}
}
