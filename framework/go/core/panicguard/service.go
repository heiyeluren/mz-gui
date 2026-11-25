package panicguard

import (
	"context"
	"strings"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
	"github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

type Service struct {
	Name     string
	Mapper   *fault.Mapper
	Reporter Reporter
	Health   HealthMarker
}

func NewService(name string) (*Service, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		failure, _ := fault.New(fault.Spec{
			Code: fault.CodeInvalidArgument, ReasonCode: "core.invalid_argument",
			MessageKey: "errors.invalid_argument", UserMessage: "操作参数无效",
		})
		return nil, failure
	}
	mapper, err := fault.NewDefaultMapper()
	if err != nil {
		return nil, err
	}
	return &Service{Name: name, Mapper: mapper}, nil
}

func (service *Service) WithHealth(health HealthMarker) *Service {
	if service == nil {
		return nil
	}
	clone := *service
	clone.Health = health
	return &clone
}

func (service *Service) WithReporter(reporter Reporter) *Service {
	if service == nil {
		return nil
	}
	clone := *service
	clone.Reporter = reporter
	return &clone
}

func AttachPanicHealth(service *Service, registry *health.Registry, checkID string, clock frameworkclock.Clock) (*Service, error) {
	if service == nil {
		failure, _ := fault.New(fault.Spec{
			Code: fault.CodeInvalidArgument, ReasonCode: "core.invalid_argument",
			MessageKey: "errors.invalid_argument", UserMessage: "操作参数无效",
		})
		return nil, failure
	}
	marker, err := health.NewPanicMarker(registry, checkID, clock)
	if err != nil {
		return nil, err
	}
	return service.WithHealth(marker), nil
}

func AttachServiceReadinessHealth(service *Service, registry *health.Registry, clock frameworkclock.Clock) (*Service, error) {
	return AttachPanicHealth(service, registry, health.ServiceReadinessCheckID, clock)
}

func (service *Service) Guard(ctx context.Context, method string, operation func(context.Context) error) error {
	return Produce(ctx, service.mapper(), service.options(method), operation)
}

func GuardResult[T any](ctx context.Context, service *Service, method string, operation func(context.Context) (T, error)) (T, error) {
	return ProduceResult(ctx, service.mapper(), service.options(method), operation)
}

func (service *Service) GuardRequest(ctx context.Context, now time.Time, meta model.RequestMeta, method string, operation func(context.Context) error) error {
	bound, cancel, err := bindGuardRequest(ctx, now, meta)
	if err != nil {
		return produceGuardRequestError(service, err)
	}
	defer cancel()
	return service.Guard(bound, method, operation)
}

func GuardRequestResult[T any](ctx context.Context, service *Service, now time.Time, meta model.RequestMeta, method string, operation func(context.Context) (T, error)) (T, error) {
	var zero T
	bound, cancel, err := bindGuardRequest(ctx, now, meta)
	if err != nil {
		return zero, produceGuardRequestError(service, err)
	}
	defer cancel()
	return GuardResult(bound, service, method, operation)
}

func bindGuardRequest(ctx context.Context, now time.Time, meta model.RequestMeta) (context.Context, context.CancelFunc, error) {
	return model.BindRequest(ctx, now, meta)
}

func produceGuardRequestError(service *Service, err error) error {
	if mapper := service.mapper(); mapper != nil {
		if produced := mapper.Produce(err); produced != nil {
			return produced
		}
	}
	return err
}

func (service *Service) mapper() *fault.Mapper {
	if service == nil {
		return nil
	}
	return service.Mapper
}

func (service *Service) options(method string) Options {
	name := "service"
	if service != nil && strings.TrimSpace(service.Name) != "" {
		name = strings.TrimSpace(service.Name)
	}
	method = strings.TrimSpace(method)
	if method == "" {
		method = "call"
	}
	options := Options{Boundary: name + "." + method, Caller: name}
	if service != nil {
		options.Reporter = service.Reporter
		options.Health = service.Health
	}
	return options
}
