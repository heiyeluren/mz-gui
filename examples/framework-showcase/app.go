package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/examples/framework-showcase/generated/appmeta"
	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
	frameworkhealth "github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
	"github.com/heiyeluren/mz-gui/framework/go/interaction/eventbus"
	"github.com/heiyeluren/mz-gui/framework/go/lifecycle"
	"github.com/heiyeluren/mz-gui/framework/go/lifecycle/wailsapp"
	"github.com/heiyeluren/mz-gui/framework/go/platform/credentials"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/resilience"
)

type ShowcaseSnapshot struct {
	LifecycleReady   bool                       `json:"lifecycle_ready"`
	CredentialSet    bool                       `json:"credential_set"`
	EventCount       uint64                     `json:"event_count"`
	FrameworkVersion model.Version              `json:"framework_version"`
	ObservedAt       model.Timestamp            `json:"observed_at"`
	Health           frameworkhealth.Report     `json:"health"`
	Lifecycle        lifecycle.Snapshot         `json:"lifecycle"`
	CircuitBreaker   resilience.BreakerSnapshot `json:"circuit_breaker"`
	RateLimiter      resilience.LimiterSnapshot `json:"rate_limiter"`
}

type showcaseEvent struct {
	Name string
}

type ShowcaseService struct {
	mu             sync.RWMutex
	ready          bool
	eventCount     uint64
	bus            *eventbus.Bus[showcaseEvent]
	subscription   *eventbus.Subscription[showcaseEvent]
	supervisor     *lifecycle.Supervisor
	credentials    *credentials.Store
	clock          frameworkclock.Clock
	health         *frameworkhealth.Registry
	panicMarker    *frameworkhealth.PanicMarker
	breaker        *resilience.Breaker
	limiter        *resilience.Limiter
	build          model.Version
	healthMu       sync.Mutex
	lastHealthAt   time.Time
	cancel         context.CancelFunc
	done           chan struct{}
	windowStateDir string
	shellDir       string
}

func NewShowcaseService() (*ShowcaseService, error) {
	if err := validateShowcaseShellEntries(); err != nil {
		return nil, err
	}
	serviceClock := frameworkclock.System{}
	initialHealthAt, err := model.NewTimestamp(serviceClock.Now())
	if err != nil {
		return nil, err
	}
	healthRegistry, panicMarker, err := newShowcaseHealth(initialHealthAt, serviceClock)
	if err != nil {
		return nil, err
	}
	build, err := model.ParseVersion(appmeta.FrameworkAPIVersion)
	if err != nil {
		return nil, err
	}
	bus, err := eventbus.New[showcaseEvent](eventbus.Options{Buffer: 8})
	if err != nil {
		return nil, err
	}
	subscription, err := bus.Subscribe()
	if err != nil {
		return nil, err
	}
	backend := &memoryCredentialBackend{values: make(map[string]string)}
	store, err := credentials.New(credentials.Options{Backend: backend, Service: appmeta.CredentialService})
	if err != nil {
		return nil, err
	}
	breaker, err := resilience.NewBreaker(resilience.BreakerConfig{
		Name: "showcase-events", FailureThreshold: 3, SuccessThreshold: 1,
		OpenTimeout: 5 * time.Second, HalfOpenMaxRequests: 1, Clock: serviceClock,
	})
	if err != nil {
		return nil, err
	}
	limiter, err := resilience.NewLimiter(resilience.LimiterConfig{
		Name: "showcase-events", Capacity: 8, RefillTokens: 2, RefillInterval: time.Second, Clock: serviceClock,
	})
	if err != nil {
		return nil, err
	}
	service := &ShowcaseService{
		bus: bus, subscription: subscription, credentials: store, clock: serviceClock,
		health: healthRegistry, panicMarker: panicMarker, breaker: breaker, limiter: limiter,
		build: build, lastHealthAt: initialHealthAt.Time(), done: make(chan struct{}),
	}
	component := &showcaseComponent{service: service}
	credentialComponent := &showcaseCredentialComponent{store: store}
	service.supervisor, err = lifecycle.NewGraph([]lifecycle.Node{
		{Component: component, Requires: []string{credentialComponent.Name()}},
		{Component: credentialComponent},
	}, lifecycle.Options{
		Clock: serviceClock,
		DefaultTimeouts: lifecycle.Timeouts{
			Start: 5 * time.Second, Ready: 5 * time.Second, Quiesce: 5 * time.Second, Stop: 5 * time.Second,
		},
	})
	if err != nil {
		return nil, err
	}
	return service, nil
}

func (*ShowcaseService) ServiceName() string { return "ShowcaseService" }

func (service *ShowcaseService) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("启动上下文无效")
	}
	if _, cancel, err := bindShowcaseRequest(ctx, time.Now().UTC()); err != nil {
		return err
	} else {
		cancel()
	}
	if err := service.supervisor.Start(ctx); err != nil {
		_ = service.setHealth("core.storage.dependency", frameworkhealth.KindDependency, frameworkhealth.StatusUnhealthy, "storage.unavailable")
		_ = service.setHealth("core.service.readiness", frameworkhealth.KindReadiness, frameworkhealth.StatusUnhealthy, "core.not_ready")
		return err
	}
	if err := service.setHealth("core.runtime.liveness", frameworkhealth.KindLiveness, frameworkhealth.StatusHealthy, frameworkhealth.ReasonOK); err != nil {
		return err
	}
	if err := service.setHealth("core.service.readiness", frameworkhealth.KindReadiness, frameworkhealth.StatusHealthy, frameworkhealth.ReasonOK); err != nil {
		return err
	}
	if err := service.persistWindowState(ctx); err != nil {
		return err
	}
	if err := service.prepareFrameworkShell(ctx); err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	service.cancel = cancel
	go service.consume(workerCtx)
	return service.setHealth("core.storage.dependency", frameworkhealth.KindDependency, frameworkhealth.StatusHealthy, frameworkhealth.ReasonOK)
}

func (service *ShowcaseService) Stop(ctx context.Context) error {
	healthErr := service.setHealth("core.service.readiness", frameworkhealth.KindReadiness, frameworkhealth.StatusUnhealthy, "core.not_ready")
	if service.cancel != nil {
		service.cancel()
		<-service.done
		service.cancel = nil
	}
	service.subscription.Cancel()
	return errors.Join(healthErr, service.bus.Close(), service.supervisor.Stop(ctx))
}

func (service *ShowcaseService) GetSnapshot(ctx context.Context) (ShowcaseSnapshot, error) {
	_, err := service.credentials.Get(ctx, "showcase/local/state")
	credentialSet := err == nil
	if err != nil && !errors.Is(err, credentials.ErrNotFound) {
		return ShowcaseSnapshot{}, err
	}
	service.mu.RLock()
	ready := service.ready
	eventCount := service.eventCount
	service.mu.RUnlock()
	observedAt, err := service.nextHealthTimestamp()
	if err != nil {
		return ShowcaseSnapshot{}, err
	}
	healthReport, err := service.health.Snapshot(observedAt, service.build)
	if err != nil {
		return ShowcaseSnapshot{}, err
	}
	return ShowcaseSnapshot{
		LifecycleReady: ready, CredentialSet: credentialSet, EventCount: eventCount,
		FrameworkVersion: service.build, ObservedAt: observedAt, Health: healthReport, Lifecycle: service.supervisor.Snapshot(),
		CircuitBreaker: service.breaker.Snapshot(), RateLimiter: service.limiter.Snapshot(),
	}, nil
}

func (service *ShowcaseService) EmitSample(ctx context.Context) (ShowcaseSnapshot, error) {
	if err := service.limiter.Acquire(1); err != nil {
		return ShowcaseSnapshot{}, err
	}
	if err := service.breaker.Execute(ctx, func(operationCtx context.Context) error {
		return service.bus.Publish(operationCtx, showcaseEvent{Name: "showcase.clicked"})
	}); err != nil {
		return ShowcaseSnapshot{}, err
	}
	return service.GetSnapshot(ctx)
}

func (*ShowcaseService) PreviewFrameworkError(context.Context) wailsapp.ErrorDTO {
	failure, err := fault.New(fault.Spec{
		Code: fault.CodeUnavailable, ReasonCode: "showcase.preview_unavailable",
		MessageKey: "errors.preview_unavailable", UserMessage: "示例服务暂时不可用",
		Retryable: true, RetryAfter: 2 * time.Second, SafeDetails: map[string]string{"component": "showcase"},
	})
	if err != nil {
		return wailsapp.MapError(err)
	}
	return wailsapp.MapError(failure)
}

func (service *ShowcaseService) consume(ctx context.Context) {
	defer close(service.done)
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-service.subscription.Events():
			if !ok {
				return
			}
			service.mu.Lock()
			service.eventCount++
			service.mu.Unlock()
		}
	}
}

func (service *ShowcaseService) setHealth(checkID string, kind frameworkhealth.Kind, status frameworkhealth.Status, reason string) error {
	observedAt, err := service.nextHealthTimestamp()
	if err != nil {
		return err
	}
	return service.health.Update(frameworkhealth.Check{
		CheckID: model.ID(checkID), Kind: kind, Status: status, ReasonCode: reason, ObservedAt: observedAt,
	})
}

func (service *ShowcaseService) nextHealthTimestamp() (model.Timestamp, error) {
	service.healthMu.Lock()
	defer service.healthMu.Unlock()
	now := service.clock.Now().UTC()
	if !now.After(service.lastHealthAt) {
		now = service.lastHealthAt.Add(time.Nanosecond)
	}
	observedAt, err := model.NewTimestamp(now)
	if err != nil {
		return "", err
	}
	service.lastHealthAt = now
	return observedAt, nil
}

type showcaseComponent struct {
	service *ShowcaseService
}

type showcaseCredentialComponent struct {
	store *credentials.Store
}

func (*showcaseCredentialComponent) Name() string                { return "credential-store" }
func (*showcaseCredentialComponent) Start(context.Context) error { return nil }
func (component *showcaseCredentialComponent) Ready(ctx context.Context) error {
	return component.store.Set(ctx, "showcase/local/state", "ready")
}
func (*showcaseCredentialComponent) Stop(context.Context) error { return nil }

func (*showcaseComponent) Name() string                { return "showcase" }
func (*showcaseComponent) Start(context.Context) error { return nil }
func (component *showcaseComponent) Ready(context.Context) error {
	component.service.mu.Lock()
	component.service.ready = true
	component.service.mu.Unlock()
	return nil
}
func (component *showcaseComponent) Stop(context.Context) error {
	component.service.mu.Lock()
	component.service.ready = false
	component.service.mu.Unlock()
	return nil
}

type memoryCredentialBackend struct {
	mu     sync.RWMutex
	values map[string]string
}

func (backend *memoryCredentialBackend) Get(service string, key string) (string, error) {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	value, exists := backend.values[service+"/"+key]
	if !exists {
		return "", credentials.ErrNotFound
	}
	return value, nil
}

func (backend *memoryCredentialBackend) Set(service string, key string, value string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.values[service+"/"+key] = value
	return nil
}

func (backend *memoryCredentialBackend) Delete(service string, key string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	lookup := service + "/" + key
	if _, exists := backend.values[lookup]; !exists {
		return credentials.ErrNotFound
	}
	delete(backend.values, lookup)
	return nil
}
