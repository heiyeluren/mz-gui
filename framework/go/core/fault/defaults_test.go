package fault

import (
	"context"
	"errors"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func TestDefaultMapperMapsCancelAndTimeout(t *testing.T) {
	mapper, err := NewDefaultMapper()
	if err != nil {
		t.Fatal(err)
	}
	cancelled := mapper.Produce(context.Canceled)
	if cancelled == nil || cancelled.Code() != CodeCancelled || cancelled.ReasonCode() != "core.operation_cancelled" {
		t.Fatalf("取消映射异常: %#v", cancelled)
	}
	timeout := mapper.Produce(context.DeadlineExceeded)
	if timeout == nil || timeout.Code() != CodeTimeout || timeout.ReasonCode() != "core.operation_timeout" || !Public(timeout).Retryable {
		t.Fatalf("超时映射异常: %#v", timeout)
	}
	invalid := mapper.Produce(model.ErrInvalidRequestMeta)
	if invalid == nil || invalid.Code() != CodeInvalidArgument || invalid.ReasonCode() != "core.invalid_argument" {
		t.Fatalf("RequestMeta 映射异常: %#v", invalid)
	}
	if err := mapper.BindDefaults(); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("重复默认绑定应拒绝: %v", err)
	}
}

func TestBindDefaultsRejectsNilMapper(t *testing.T) {
	var mapper *Mapper
	if err := mapper.BindDefaults(); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("空 Mapper 应拒绝: %v", err)
	}
}

func TestBindReasonUsesErrorCatalog(t *testing.T) {
	catalog, err := DecodeCatalog([]byte(`{"schema_version":1,"release":"1.0.0","codes":[{"code":"INVALID_ARGUMENT","grpc_code":"InvalidArgument"},{"code":"CANCELLED","grpc_code":"Canceled"},{"code":"TIMEOUT","grpc_code":"DeadlineExceeded"}],"reasons":[{"reason_code":"core.invalid_argument","code":"INVALID_ARGUMENT","message_key":"errors.invalid_argument","user_message":"操作参数无效","retryable":false},{"reason_code":"core.operation_cancelled","code":"CANCELLED","message_key":"errors.operation_cancelled","user_message":"操作已取消","retryable":false},{"reason_code":"core.operation_timeout","code":"TIMEOUT","message_key":"errors.operation_timeout","user_message":"操作已超时","retryable":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.Reason("core.operation_timeout"); !ok {
		t.Fatal("必须能按 reason 查找")
	}
	mapper := NewMapper()
	if err := mapper.BindDefaultsFromCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	if got := mapper.Produce(context.Canceled); got == nil || got.ReasonCode() != "core.operation_cancelled" {
		t.Fatalf("catalog 取消映射异常: %#v", got)
	}
	if got := mapper.Produce(model.ErrInvalidRequestMeta); got == nil || got.ReasonCode() != "core.invalid_argument" {
		t.Fatalf("catalog RequestMeta 映射异常: %#v", got)
	}
	if err := mapper.BindReason(context.Canceled, catalog, "core.missing"); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("未知 reason 应拒绝: %v", err)
	}
}
