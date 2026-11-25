package wailsapp

import (
	"context"

	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
	"github.com/heiyeluren/mz-gui/framework/go/core/panicguard"
)

type ErrorDTO = fault.PublicError

func MapError(err error) ErrorDTO {
	return fault.Public(err)
}

func GuardedCall(ctx context.Context, options panicguard.Options, operation func(context.Context) error) ErrorDTO {
	return GuardedProduce(ctx, nil, options, operation)
}

func GuardedProduce(ctx context.Context, mapper *fault.Mapper, options panicguard.Options, operation func(context.Context) error) ErrorDTO {
	if ctx == nil {
		ctx = context.Background()
	}
	err := panicguard.Run(ctx, options, operation)
	if mapper != nil {
		err = mapper.Produce(err)
	}
	return MapError(err)
}
