package health

import (
	"context"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

type PanicMarker struct {
	registry *Registry
	checkID  string
	kind     Kind
	clock    frameworkclock.Clock
}

const ServiceReadinessCheckID = "core.service.readiness"

func NewServicePanicMarker(registry *Registry, clock frameworkclock.Clock) (*PanicMarker, error) {
	return NewPanicMarker(registry, ServiceReadinessCheckID, clock)
}

func NewPanicMarker(registry *Registry, checkID string, clock frameworkclock.Clock) (*PanicMarker, error) {
	if registry == nil || clock == nil {
		return nil, ErrInvalidRegistry
	}
	definition, exists := registry.definition(checkID)
	if !exists {
		return nil, ErrCheckNotRegistered
	}
	return &PanicMarker{registry: registry, checkID: checkID, kind: definition.Kind, clock: clock}, nil
}

func (marker *PanicMarker) MarkDegraded(ctx context.Context, _ string, reason string) {
	if marker == nil || ctx == nil || ctx.Err() != nil {
		return
	}
	observedAt, err := model.NewTimestamp(marker.clock.Now())
	if err != nil {
		return
	}
	_ = marker.registry.Update(Check{
		CheckID: model.ID(marker.checkID), Kind: marker.kind, Status: StatusDegraded,
		ReasonCode: reason, ObservedAt: observedAt,
	})
}
