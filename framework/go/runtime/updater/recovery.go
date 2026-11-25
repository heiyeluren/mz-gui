package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maximumSessionStateBytes = 64 << 10

type ProcessSetRecoveryOptions struct {
	Root                string
	Components          []LiveComponent
	AllowedInstallRoots []string
	Replace             func(source, dest string) error
}

// RecoverProcessSet 使用固定组件清单和 previous 备份恢复崩溃时中断的切换。
func RecoverProcessSet(ctx context.Context, options ProcessSetRecoveryOptions) (SessionState, error) {
	if ctx == nil || ctx.Err() != nil || !filepath.IsAbs(options.Root) || len(options.Components) < 2 || len(options.AllowedInstallRoots) == 0 {
		return SessionState{}, ErrInvalidSession
	}
	if err := requireSafeDirectory(options.Root); err != nil {
		return SessionState{}, ErrInvalidSession
	}
	statePath := filepath.Join(options.Root, "session.json")
	state, err := loadSessionState(statePath)
	if err != nil {
		return SessionState{}, err
	}
	if state.SchemaVersion != "mzgui.update.session.v1" || state.SelectedKind != "process-set" {
		return SessionState{}, ErrInvalidSession
	}
	switch state.Phase {
	case PhaseSwitching, PhaseValidating, PhaseRepairRequired:
	case PhaseCommitted, PhaseRolledBack:
		return state, nil
	default:
		state.Phase = PhaseRolledBack
		state.Reason = "interrupted_before_switch"
		if err := persistRecoveredState(statePath, state); err != nil {
			return state, err
		}
		return state, nil
	}

	items, err := prepareRecoveryItems(options)
	if err != nil {
		return SessionState{}, err
	}
	replace := options.Replace
	if replace == nil {
		replace = replaceFile
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return state, err
		}
		restoreCopy := filepath.Join(options.Root, "incoming", "restore-"+item.name+".bin")
		if err := copyExclusive(item.previous, restoreCopy, maximumArtifactBytes); err != nil {
			return markRecoveryRepair(statePath, state, "recovery_copy_failed")
		}
		if err := replace(restoreCopy, item.livePath); err != nil {
			return markRecoveryRepair(statePath, state, "recovery_switch_failed")
		}
	}
	for _, item := range items {
		if err := verifyFileHash(item.livePath, item.currentSHA256); err != nil {
			return markRecoveryRepair(statePath, state, "recovery_validate_failed")
		}
	}
	state.Phase = PhaseRolledBack
	state.Reason = "interrupted_switch_recovered"
	if err := persistRecoveredState(statePath, state); err != nil {
		return state, err
	}
	return state, nil
}

type recoveryItem struct {
	name          string
	livePath      string
	previous      string
	currentSHA256 string
}

func prepareRecoveryItems(options ProcessSetRecoveryOptions) ([]recoveryItem, error) {
	seen := make(map[string]LiveComponent, len(options.Components))
	for _, component := range options.Components {
		name := strings.TrimSpace(component.Component)
		if name == "" || !validProcessComponent(name) || !filepath.IsAbs(component.LivePath) || !sha256Pattern.MatchString(strings.ToLower(component.CurrentSHA256)) {
			return nil, ErrInvalidProcessSet
		}
		if _, exists := seen[name]; exists {
			return nil, ErrInvalidProcessSet
		}
		seen[name] = component
	}
	if _, ok := seen[ComponentDesktop]; !ok {
		return nil, ErrInvalidProcessSet
	}
	if _, ok := seen[ComponentAgentd]; !ok {
		return nil, ErrInvalidProcessSet
	}
	installRoot := filepath.Dir(seen[ComponentDesktop].LivePath)
	allowed := false
	for _, root := range options.AllowedInstallRoots {
		root = filepath.Clean(strings.TrimSpace(root))
		if filepath.IsAbs(root) && pathWithinRoot(root, installRoot) && requireSafeDirectory(root) == nil {
			allowed = true
			break
		}
	}
	if !allowed || requireSafeDirectory(installRoot) != nil {
		return nil, ErrInvalidProcessSet
	}
	previousRoot := filepath.Join(options.Root, "previous")
	if err := requireSafeDirectory(previousRoot); err != nil {
		return nil, ErrInvalidSession
	}
	if err := os.MkdirAll(filepath.Join(options.Root, "incoming"), 0o700); err != nil {
		return nil, ErrInvalidSession
	}
	if err := requireSafeDirectory(filepath.Join(options.Root, "incoming")); err != nil {
		return nil, ErrInvalidSession
	}
	items := make([]recoveryItem, 0, len(seen))
	for _, name := range []string{ComponentDesktop, ComponentAgentd, ComponentHelper} {
		component, ok := seen[name]
		if !ok {
			continue
		}
		if !pathWithinRoot(installRoot, component.LivePath) || component.LivePath == installRoot {
			return nil, ErrInvalidProcessSet
		}
		previous := filepath.Join(previousRoot, name+".bin")
		if _, err := absRegularFile(previous); err != nil || verifyFileHash(previous, component.CurrentSHA256) != nil {
			return nil, ErrInvalidSession
		}
		if _, err := absRegularFile(component.LivePath); err != nil {
			return nil, ErrInvalidSession
		}
		items = append(items, recoveryItem{name: name, livePath: component.LivePath, previous: previous, currentSHA256: strings.ToLower(component.CurrentSHA256)})
	}
	return items, nil
}

func loadSessionState(path string) (SessionState, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > maximumSessionStateBytes {
		return SessionState{}, ErrInvalidSession
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionState{}, ErrInvalidSession
	}
	var state SessionState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return SessionState{}, ErrInvalidSession
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return SessionState{}, ErrInvalidSession
	}
	return state, nil
}

func persistRecoveredState(path string, state SessionState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return ErrInvalidSession
	}
	return writeSessionStateAtomic(path, data)
}

func markRecoveryRepair(path string, state SessionState, reason string) (SessionState, error) {
	state.Phase = PhaseRepairRequired
	state.Reason = reason
	if err := persistRecoveredState(path, state); err != nil {
		return state, fmt.Errorf("%w: %v", ErrUpdateRepairRequired, err)
	}
	return state, ErrUpdateRepairRequired
}
