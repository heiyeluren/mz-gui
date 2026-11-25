package sidecargroup

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/lifecycle"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/managedprocess"
)

type Group struct {
	opMu         sync.Mutex
	supervisor   *lifecycle.Supervisor
	declarations []Declaration
	components   []*processComponent
}

type processComponent struct {
	declaration Declaration
	manager     ProcessManager
}

func New(declarations []Declaration, options Options) (*Group, error) {
	if len(declarations) == 0 || options.MaxParallelStarts < 0 {
		return nil, ErrInvalidGroup
	}
	factory := options.Factory
	if factory == nil {
		factory = func(Declaration) ProcessManager { return managedprocess.New() }
	}
	frozen := make([]Declaration, 0, len(declarations))
	components := make([]*processComponent, 0, len(declarations))
	nodes := make([]lifecycle.Node, 0, len(declarations))
	for _, declaration := range declarations {
		declaration = cloneDeclaration(declaration)
		if declaration.ID == "" || (declaration.Process.ID != "" && declaration.Process.ID != declaration.ID) {
			return nil, ErrInvalidGroup
		}
		declaration.Process.ID = declaration.ID
		manager, factoryOK := createManager(factory, cloneDeclaration(declaration))
		if !factoryOK || isNilManager(manager) {
			return nil, ErrInvalidGroup
		}
		component := &processComponent{declaration: declaration, manager: manager}
		frozen = append(frozen, declaration)
		components = append(components, component)
		nodes = append(nodes, lifecycle.Node{
			Component: component, Requires: declaration.Requires, OptionalRequires: declaration.OptionalRequires,
			FailurePolicy: declaration.FailurePolicy, Timeouts: declaration.Timeouts,
		})
	}
	lifecycleOptions := options.Lifecycle
	if options.MaxParallelStarts != 0 {
		if lifecycleOptions.MaxParallelStarts != 0 && lifecycleOptions.MaxParallelStarts != options.MaxParallelStarts {
			return nil, ErrInvalidGroup
		}
		lifecycleOptions.MaxParallelStarts = options.MaxParallelStarts
	}
	supervisor, err := lifecycle.NewGraph(nodes, lifecycleOptions)
	if err != nil {
		return nil, errors.Join(ErrInvalidGroup, err)
	}
	return &Group{supervisor: supervisor, declarations: frozen, components: components}, nil
}

func createManager(factory Factory, declaration Declaration) (manager ProcessManager, ok bool) {
	defer func() {
		if recover() != nil {
			manager = nil
			ok = false
		}
	}()
	return factory(declaration), true
}

func (group *Group) Start(ctx context.Context) error {
	if group == nil || ctx == nil {
		return ErrInvalidGroup
	}
	group.opMu.Lock()
	defer group.opMu.Unlock()
	return group.supervisor.Start(ctx)
}

func (group *Group) Stop(ctx context.Context) error {
	if group == nil || ctx == nil {
		return ErrInvalidGroup
	}
	group.opMu.Lock()
	defer group.opMu.Unlock()
	return group.supervisor.Stop(ctx)
}

// Restart 只恢复指定的受管进程；停止失败时保留所有权，不另起新进程。
func (group *Group) Restart(ctx context.Context, id string) error {
	if group == nil || ctx == nil {
		return ErrInvalidGroup
	}
	group.opMu.Lock()
	defer group.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if group.supervisor.Snapshot().Closed {
		return lifecycle.ErrClosed
	}
	for _, component := range group.components {
		if component.declaration.ID != id {
			continue
		}
		state := component.manager.Status().State
		if state == managedprocess.StateCrashLoop || state == managedprocess.StateIncompatible {
			retry, ok := component.manager.(interface {
				Retry(context.Context) (managedprocess.Snapshot, error)
			})
			if !ok {
				return ErrNotReady
			}
			if _, err := retry.Retry(ctx); err != nil {
				return err
			}
		} else {
			if err := component.Stop(ctx); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := component.Start(ctx); err != nil {
				return err
			}
		}
		// 初次启动失败时重新登记已启动节点，确保后续退出仍能停止它们。
		if !group.supervisor.Snapshot().Running {
			if err := group.supervisor.Start(ctx); err != nil {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), component.stopTimeout())
				defer cancel()
				return errors.Join(err, component.Stop(cleanup))
			}
			return nil
		}
		return component.Ready(ctx)
	}
	return ErrInvalidGroup
}

func (group *Group) Snapshot() Snapshot {
	if group == nil || group.supervisor == nil {
		return Snapshot{}
	}
	lifecycleSnapshot := group.supervisor.Snapshot()
	result := Snapshot{
		Running: lifecycleSnapshot.Running, Ready: lifecycleSnapshot.Running, Degraded: lifecycleSnapshot.Degraded,
		Lifecycle: lifecycleSnapshot, Sidecars: make([]SidecarSnapshot, 0, len(group.components)),
	}
	for index, component := range group.components {
		process := component.manager.Status()
		required := group.declarations[index].FailurePolicy != lifecycle.FailureOptional
		sidecar := SidecarSnapshot{
			ID: component.declaration.ID, Required: required, State: process.State, Running: process.Running,
			Ready: process.Ready, Degraded: process.Degraded, CrashLoop: process.CrashLoop,
			Incompatible: process.Incompatible, PID: process.PID, Version: process.ProcessVersion,
			ProtocolMajor: process.ProtocolMajor, ProtocolMinor: process.ProtocolMinor,
			Capabilities: append([]string(nil), process.Capabilities...),
		}
		result.Sidecars = append(result.Sidecars, sidecar)
		result.CrashLoop = result.CrashLoop || sidecar.CrashLoop
		result.Incompatible = result.Incompatible || sidecar.Incompatible
		if sidecar.Degraded || sidecar.CrashLoop || sidecar.Incompatible || (lifecycleSnapshot.Running && !sidecar.Ready) {
			result.Degraded = true
		}
		if required && !sidecar.Ready {
			result.Ready = false
		}
	}
	return result
}

func (component *processComponent) Name() string { return component.declaration.ID }

func (component *processComponent) Start(ctx context.Context) error {
	_, err := component.manager.Start(ctx, cloneSpec(component.declaration.Process))
	if err == nil {
		return nil
	}
	state := component.manager.Status()
	if state.Running || state.State == managedprocess.StateStarting || state.State == managedprocess.StateBackoff || state.State == managedprocess.StateStopping {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), component.stopTimeout())
		defer cancel()
		cleanupErr := component.manager.Stop(cleanupContext)
		if cleanupErr != nil && !errors.Is(cleanupErr, managedprocess.ErrNotRunning) && !errors.Is(cleanupErr, managedprocess.ErrDisabled) {
			return errors.Join(err, cleanupErr)
		}
	}
	return err
}

func (component *processComponent) Ready(context.Context) error {
	snapshot := component.manager.Status()
	switch {
	case snapshot.Incompatible || snapshot.State == managedprocess.StateIncompatible:
		return managedprocess.ErrIncompatible
	case snapshot.CrashLoop || snapshot.State == managedprocess.StateCrashLoop:
		return managedprocess.ErrCrashLoop
	case snapshot.Ready && snapshot.Running:
		return nil
	default:
		return ErrNotReady
	}
}

func (component *processComponent) Stop(ctx context.Context) error {
	err := component.manager.Stop(ctx)
	if errors.Is(err, managedprocess.ErrNotRunning) || errors.Is(err, managedprocess.ErrDisabled) {
		return nil
	}
	return err
}

func (component *processComponent) stopTimeout() time.Duration {
	if component.declaration.Timeouts.Stop > 0 {
		return component.declaration.Timeouts.Stop
	}
	return 30 * time.Second
}

func cloneDeclaration(declaration Declaration) Declaration {
	declaration.Process = cloneSpec(declaration.Process)
	declaration.Requires = append([]string(nil), declaration.Requires...)
	declaration.OptionalRequires = append([]string(nil), declaration.OptionalRequires...)
	return declaration
}

func cloneSpec(spec managedprocess.Spec) managedprocess.Spec {
	spec.Arguments = append([]string(nil), spec.Arguments...)
	if spec.Environment != nil {
		environment := make(map[string]string, len(spec.Environment))
		for key, value := range spec.Environment {
			environment[key] = value
		}
		spec.Environment = environment
	}
	return spec
}
