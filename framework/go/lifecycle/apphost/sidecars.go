package apphost

import (
	"errors"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/heiyeluren/mz-gui/framework/go/core/appmanifest"
	"github.com/heiyeluren/mz-gui/framework/go/lifecycle"
	"github.com/heiyeluren/mz-gui/framework/go/lifecycle/sidecargroup"
	"github.com/heiyeluren/mz-gui/framework/go/platform/startup"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/managedprocess"
)

type SiblingSidecarOptions struct {
	DesktopExecutable string
	GOOS              string
	Processes         appmanifest.Processes
	Restart           managedprocess.RestartConfig
	Timeouts          lifecycle.Timeouts
	ProcessEnv        func(processID string) map[string]string
	ProcessProbe      func(processID string) managedprocess.ProbeConfig
	ProcessObserver   managedprocess.Observer
}

func SiblingSidecarDeclarations(options SiblingSidecarOptions) ([]sidecargroup.Declaration, error) {
	goos := options.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if !filepath.IsAbs(options.DesktopExecutable) || options.Processes.Agentd == "" {
		return nil, ErrInvalidHost
	}
	type role struct {
		id       string
		required bool
		requires []string
	}
	roles := []role{{id: options.Processes.Agentd, required: true}}
	if options.Processes.BrowserHost != "" {
		roles = append(roles, role{id: options.Processes.BrowserHost, requires: []string{options.Processes.Agentd}})
	}
	if options.Processes.Helper != "" {
		roles = append(roles, role{id: options.Processes.Helper, requires: []string{options.Processes.Agentd}})
	}
	seen := map[string]struct{}{}
	declarations := make([]sidecargroup.Declaration, 0, len(roles))
	for _, item := range roles {
		if _, exists := seen[item.id]; exists {
			return nil, ErrInvalidHost
		}
		seen[item.id] = struct{}{}
		executable, err := startup.ResolveSiblingExecutable(options.DesktopExecutable, item.id, goos)
		if err != nil {
			return nil, errors.Join(ErrInvalidHost, err)
		}
		policy := lifecycle.FailureOptional
		if item.required {
			policy = lifecycle.FailureRequired
		}
		process := managedprocess.Spec{
			ID: item.id, Executable: executable, Restart: options.Restart,
			Environment: options.processEnv(item.id),
			Probe:       options.processProbe(item.id),
			Observer:    options.ProcessObserver,
		}
		declarations = append(declarations, sidecargroup.Declaration{
			ID: item.id, Requires: append([]string(nil), item.requires...), FailurePolicy: policy, Timeouts: options.Timeouts,
			Process: process,
		})
	}
	sort.Slice(declarations, func(left, right int) bool {
		if declarations[left].ID == options.Processes.Agentd {
			return true
		}
		if declarations[right].ID == options.Processes.Agentd {
			return false
		}
		return declarations[left].ID < declarations[right].ID
	})
	return declarations, nil
}

func (options SiblingSidecarOptions) processEnv(processID string) map[string]string {
	if options.ProcessEnv == nil {
		return nil
	}
	env := options.ProcessEnv(processID)
	if len(env) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(env))
	for key, value := range env {
		cloned[key] = value
	}
	return cloned
}

func (options SiblingSidecarOptions) processProbe(processID string) managedprocess.ProbeConfig {
	if options.ProcessProbe == nil {
		return managedprocess.ProbeConfig{}
	}
	return options.ProcessProbe(processID)
}

func NewSiblingSidecarGroup(options SiblingSidecarOptions, groupOptions sidecargroup.Options) (*sidecargroup.Group, error) {
	declarations, err := SiblingSidecarDeclarations(options)
	if err != nil {
		return nil, err
	}
	return sidecargroup.New(declarations, groupOptions)
}
