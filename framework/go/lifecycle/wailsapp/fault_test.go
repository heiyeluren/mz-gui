package wailsapp

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
	"github.com/heiyeluren/mz-gui/framework/go/core/panicguard"
)

func TestMapErrorReturnsStableWailsDTO(t *testing.T) {
	failure, err := fault.New(fault.Spec{
		Code: fault.CodePermissionDenied, ReasonCode: "security.capability_denied",
		MessageKey: "errors.capability_denied", UserMessage: "当前操作未获授权",
		RequestID: "request-1", Cause: errors.New("token=secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	dto := MapError(failure)
	if dto.Code != fault.CodePermissionDenied || dto.ReasonCode != "security.capability_denied" || dto.RequestID != "request-1" {
		t.Fatalf("Wails 错误 DTO 异常: %#v", dto)
	}
	if strings.Contains(dto.UserMessage, "secret") {
		t.Fatal("Wails 错误 DTO 泄露内部 cause")
	}
}

func TestGuardedCallProducesPublicErrorAndContainsPanic(t *testing.T) {
	dto := GuardedCall(context.Background(), panicguard.Options{Boundary: "wails.settings"}, func(context.Context) error {
		panic("token=secret")
	})
	if dto.Code != fault.CodeInternal || strings.Contains(dto.UserMessage, "secret") || dto.SafeDetails["fingerprint"] == "" {
		t.Fatalf("Wails panic 边界异常: %#v", dto)
	}

	failure, err := fault.New(fault.Spec{
		Code: fault.CodeNotFound, ReasonCode: "core.resource_missing",
		MessageKey: "errors.not_found", UserMessage: "资源不存在", RequestID: "request-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	dto = GuardedCall(context.Background(), panicguard.Options{Boundary: "wails.settings"}, func(context.Context) error {
		return failure
	})
	if dto.Code != fault.CodeNotFound || dto.RequestID != "request-2" {
		t.Fatalf("Wails 业务错误未保留: %#v", dto)
	}

	empty := GuardedCall(context.Background(), panicguard.Options{Boundary: "wails.settings"}, func(context.Context) error {
		return nil
	})
	if empty.Code != "" {
		t.Fatalf("成功调用不应返回错误: %#v", empty)
	}
}

func TestGuardedProduceMapsSentinelThroughMapper(t *testing.T) {
	mapper := fault.NewMapper()
	if err := mapper.Bind(os.ErrNotExist, fault.Spec{
		Code: fault.CodeNotFound, ReasonCode: "core.resource_missing",
		MessageKey: "errors.not_found", UserMessage: "资源不存在",
	}); err != nil {
		t.Fatal(err)
	}
	dto := GuardedProduce(context.Background(), mapper, panicguard.Options{Boundary: "wails.settings"}, func(context.Context) error {
		return os.ErrNotExist
	})
	if dto.Code != fault.CodeNotFound || dto.ReasonCode != "core.resource_missing" {
		t.Fatalf("Mapper 生产错误异常: %#v", dto)
	}
}
