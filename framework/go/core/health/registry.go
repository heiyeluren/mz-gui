package health

import (
	"fmt"
	"sort"
	"sync"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

type Registry struct {
	mu          sync.RWMutex
	definitions map[string]Definition
	checks      map[string]Check
}

func NewBuiltinRegistry(initialObservedAt model.Timestamp) (*Registry, error) {
	catalog, err := BuiltinCatalog()
	if err != nil {
		return nil, err
	}
	return NewRegistry(catalog.Definitions(), initialObservedAt)
}

func NewRegistry(definitions []Definition, initialObservedAt model.Timestamp) (*Registry, error) {
	if _, err := model.ParseTimestamp(string(initialObservedAt)); err != nil || len(definitions) == 0 {
		return nil, ErrInvalidRegistry
	}
	registry := &Registry{
		definitions: make(map[string]Definition, len(definitions)),
		checks:      make(map[string]Check, len(definitions)),
	}
	for _, definition := range definitions {
		if err := definition.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRegistry, err)
		}
		if _, exists := registry.definitions[definition.CheckID]; exists {
			return nil, fmt.Errorf("%w: 重复 check_id %s", ErrInvalidRegistry, definition.CheckID)
		}
		definition.ReasonCodes = append([]string(nil), definition.ReasonCodes...)
		registry.definitions[definition.CheckID] = definition
		registry.checks[definition.CheckID] = Check{
			CheckID: model.ID(definition.CheckID), Kind: definition.Kind, Status: StatusUnknown,
			ReasonCode: ReasonNotObserved, ObservedAt: initialObservedAt,
		}
	}
	return registry, nil
}

func (registry *Registry) Update(check Check) error {
	if registry == nil {
		return ErrInvalidRegistry
	}
	if err := check.Validate(); err != nil {
		return err
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	definition, exists := registry.definitions[check.CheckID.String()]
	if !exists {
		return ErrCheckNotRegistered
	}
	if definition.Kind != check.Kind {
		return ErrKindMismatch
	}
	if !definition.allowsReason(check.ReasonCode) {
		return ErrReasonNotRegistered
	}
	current := registry.checks[check.CheckID.String()]
	comparison := check.ObservedAt.Time().Compare(current.ObservedAt.Time())
	if comparison < 0 {
		return ErrStaleObservation
	}
	if comparison == 0 {
		if current == check {
			return nil
		}
		return ErrObservationConflict
	}
	registry.checks[check.CheckID.String()] = check
	return nil
}

func (registry *Registry) Snapshot(observedAt model.Timestamp, build model.Version) (Report, error) {
	return registry.snapshot(nil, observedAt, build)
}

func (registry *Registry) SnapshotKind(kind Kind, observedAt model.Timestamp, build model.Version) (Report, error) {
	if !kind.Valid() {
		return Report{}, ErrInvalidCheck
	}
	return registry.snapshot(&kind, observedAt, build)
}

func (registry *Registry) snapshot(kind *Kind, observedAt model.Timestamp, build model.Version) (Report, error) {
	if registry == nil {
		return Report{}, ErrInvalidRegistry
	}
	if _, err := model.ParseTimestamp(string(observedAt)); err != nil {
		return Report{}, ErrInvalidCheck
	}
	if _, err := model.ParseVersion(build.String()); err != nil {
		return Report{}, ErrInvalidCheck
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	checks := make([]Check, 0, len(registry.checks))
	definitions := make(map[string]Definition, len(registry.definitions))
	for checkID, check := range registry.checks {
		definition := registry.definitions[checkID]
		if kind != nil && definition.Kind != *kind {
			continue
		}
		if observedAt.Time().Before(check.ObservedAt.Time()) {
			return Report{}, ErrStaleObservation
		}
		checks = append(checks, check)
		definitions[checkID] = definition
	}
	if len(checks) == 0 {
		return Report{}, ErrInvalidRegistry
	}
	sort.Slice(checks, func(left int, right int) bool {
		return checks[left].CheckID.String() < checks[right].CheckID.String()
	})
	return Report{
		Status: aggregate(checks, definitions), Checks: checks, ObservedAt: observedAt, Build: build,
	}, nil
}

func (registry *Registry) definition(checkID string) (Definition, bool) {
	if registry == nil {
		return Definition{}, false
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	definition, exists := registry.definitions[checkID]
	return definition, exists
}

func aggregate(checks []Check, definitions map[string]Definition) Status {
	hasRequiredUnknown := false
	hasDegradation := false
	for _, check := range checks {
		definition := definitions[check.CheckID.String()]
		if definition.Required && check.Status == StatusUnhealthy && (check.Kind == KindLiveness || check.Kind == KindReadiness) {
			return StatusUnhealthy
		}
		if definition.Required && check.Status == StatusUnknown {
			hasRequiredUnknown = true
		}
		if check.Status == StatusDegraded || check.Status == StatusUnhealthy {
			hasDegradation = true
		}
	}
	if hasRequiredUnknown {
		return StatusUnknown
	}
	if hasDegradation {
		return StatusDegraded
	}
	return StatusHealthy
}
