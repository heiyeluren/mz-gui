package fault

import (
	"context"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func NewDefaultMapper() (*Mapper, error) {
	mapper := NewMapper()
	if err := mapper.BindDefaults(); err != nil {
		return nil, err
	}
	return mapper, nil
}

func (mapper *Mapper) BindDefaults() error {
	if mapper == nil {
		return ErrInvalidBinding
	}
	defaults := []Binding{
		{Match: context.Canceled, Spec: Spec{
			Code: CodeCancelled, ReasonCode: "core.operation_cancelled",
			MessageKey: "errors.operation_cancelled", UserMessage: "操作已取消",
		}},
		{Match: context.DeadlineExceeded, Spec: Spec{
			Code: CodeTimeout, ReasonCode: "core.operation_timeout",
			MessageKey: "errors.operation_timeout", UserMessage: "操作已超时", Retryable: true,
		}},
		{Match: model.ErrInvalidRequestMeta, Spec: Spec{
			Code: CodeInvalidArgument, ReasonCode: "core.invalid_argument",
			MessageKey: "errors.invalid_argument", UserMessage: "操作参数无效",
		}},
	}
	for _, binding := range defaults {
		if err := mapper.Bind(binding.Match, binding.Spec); err != nil {
			return err
		}
	}
	return nil
}

func (mapper *Mapper) BindReason(match error, catalog Catalog, reasonCode string) error {
	definition, ok := catalog.Reason(reasonCode)
	if !ok {
		return ErrInvalidBinding
	}
	return mapper.Bind(match, Spec{
		Code: definition.Code, ReasonCode: definition.ReasonCode,
		MessageKey: definition.MessageKey, UserMessage: definition.UserMessage, Retryable: definition.Retryable,
	})
}

func (mapper *Mapper) BindDefaultsFromCatalog(catalog Catalog) error {
	if err := mapper.BindReason(context.Canceled, catalog, "core.operation_cancelled"); err != nil {
		return err
	}
	if err := mapper.BindReason(context.DeadlineExceeded, catalog, "core.operation_timeout"); err != nil {
		return err
	}
	return mapper.BindReason(model.ErrInvalidRequestMeta, catalog, "core.invalid_argument")
}
