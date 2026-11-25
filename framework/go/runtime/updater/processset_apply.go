package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type LiveComponent struct {
	Component     string
	LivePath      string
	CurrentSHA256 string
}

type ProcessSetOptions struct {
	Root                string
	Manifest            Manifest
	OS                  string
	Arch                string
	AllowedInstallRoots []string
	Components          []LiveComponent
	Current             ProcessSet
	ReceiptPath         string
	SBOMSHA256          string
	Fetch               func(context.Context, Artifact, string) error
	Quiesce             func(context.Context) error
	Validate            func(context.Context, map[string]string) error
	Replace             func(source, dest string) error
	WriteState          func(path string, data []byte) error
}

type preparedComponent struct {
	name     string
	livePath string
	artifact Artifact
	staged   string
	previous string
	relPath  string
}

// ApplyProcessSet 一次切换同一签名清单的 desktop+agentd（及可选 helper）；失败恢复全部旧制品。
func ApplyProcessSet(ctx context.Context, options ProcessSetOptions) (SessionState, error) {
	if !options.Manifest.Verified() {
		return SessionState{}, ErrUntrustedManifest
	}
	if err := RequireStartableProcessSet(options.Current); err != nil {
		return SessionState{}, err
	}
	prepared, installRoot, err := prepareProcessSet(options)
	if err != nil {
		return SessionState{}, err
	}
	session, err := newProcessSetSession(ctx, options)
	if err != nil {
		return SessionState{}, err
	}
	if err := session.persist(PhaseChecking, ""); err != nil {
		return session.state, err
	}
	if err := session.persist(PhaseDownloading, ""); err != nil {
		return session.state, err
	}
	for index := range prepared {
		item := &prepared[index]
		item.staged = filepath.Join(session.incomingDir, item.name+".bin")
		item.previous = filepath.Join(session.previousDir, item.name+".bin")
		if err := fetchAndVerify(ctx, options.Fetch, item.artifact, item.staged); err != nil {
			return session.failBeforeSwitch("download_failed", err)
		}
		session.state.TargetSHA256 = strings.ToLower(item.artifact.SHA256)
	}
	if err := session.persist(PhaseVerified, ""); err != nil {
		return session.state, err
	}
	if err := session.persist(PhaseStaged, ""); err != nil {
		return session.state, err
	}
	if err := session.persist(PhaseQuiescing, ""); err != nil {
		return session.state, err
	}
	if options.Quiesce != nil {
		if err := options.Quiesce(ctx); err != nil {
			return session.failBeforeSwitch("quiesce_failed", err)
		}
	}
	for _, item := range prepared {
		if err := copyExclusive(item.livePath, item.previous, maximumArtifactBytes); err != nil {
			return session.failBeforeSwitch("backup_failed", err)
		}
	}
	if err := session.persist(PhaseSwitching, ""); err != nil {
		return session.state, err
	}
	replace := options.Replace
	if replace == nil {
		replace = replaceFile
	}
	if err := switchPreparedComponents(prepared, replace); err != nil {
		if restoreErr := restorePreparedComponents(prepared, replace); restoreErr != nil {
			return session.failRepair("switch_failed")
		}
		return session.failRolledBack("switch_failed")
	}
	if err := session.persist(PhaseValidating, ""); err != nil {
		if restoreErr := restorePreparedComponents(prepared, replace); restoreErr != nil {
			return session.failRepair("journal_failed")
		}
		return session.failRolledBack("journal_failed")
	}
	nextSet := processSetFromManifest(options.Manifest, options.Current)
	if err := RequireStartableProcessSet(nextSet); err != nil {
		if restoreErr := restorePreparedComponents(prepared, replace); restoreErr != nil {
			return session.failRepair("validate_failed")
		}
		return session.failRolledBack("validate_failed")
	}
	if options.Validate != nil {
		live := make(map[string]string, len(prepared))
		for _, item := range prepared {
			live[item.name] = item.livePath
		}
		if err := options.Validate(ctx, live); err != nil {
			if restoreErr := restorePreparedComponents(prepared, replace); restoreErr != nil {
				return session.failRepair("validate_failed")
			}
			return session.failRolledBack("validate_failed")
		}
	}
	relPaths := make([]string, 0, len(prepared))
	for _, item := range prepared {
		relPaths = append(relPaths, item.relPath)
	}
	receiptBefore, err := captureReceiptSnapshot(options.ReceiptPath)
	if err != nil {
		if restoreErr := restorePreparedComponents(prepared, replace); restoreErr != nil {
			return session.failRepair("receipt_backup_failed")
		}
		return session.failRolledBack("receipt_backup_failed")
	}
	if err := session.writeProcessReceipt(installRoot, relPaths); err != nil {
		restoreErr := restorePreparedComponents(prepared, replace)
		receiptErr := restoreReceiptSnapshot(receiptBefore)
		if restoreErr != nil || receiptErr != nil {
			return session.failRepair("receipt_failed")
		}
		return session.failRolledBack("receipt_failed")
	}
	if err := session.persist(PhaseCommitted, ""); err != nil {
		restoreErr := restorePreparedComponents(prepared, replace)
		receiptErr := restoreReceiptSnapshot(receiptBefore)
		if restoreErr != nil || receiptErr != nil {
			return session.failRepair("commit_journal_failed")
		}
		return session.failRolledBack("commit_journal_failed")
	}
	return session.state, nil
}

func newProcessSetSession(ctx context.Context, options ProcessSetOptions) (*session, error) {
	if ctx == nil || options.Fetch == nil || options.Root == "" || !filepath.IsAbs(options.Root) ||
		options.OS == "" || options.Arch == "" {
		return nil, ErrInvalidSession
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	desktopSHA := ""
	for _, component := range options.Components {
		if component.Component == ComponentDesktop {
			desktopSHA = strings.ToLower(strings.TrimSpace(component.CurrentSHA256))
		}
	}
	if !sha256Pattern.MatchString(desktopSHA) {
		return nil, ErrInvalidSession
	}
	incoming := filepath.Join(options.Root, "incoming")
	previous := filepath.Join(options.Root, "previous")
	for _, directory := range []string{incoming, previous} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSession, err)
		}
	}
	return &session{
		options: SessionOptions{
			Root: options.Root, Manifest: options.Manifest, OS: options.OS, Arch: options.Arch,
			ReceiptPath: options.ReceiptPath, SBOMSHA256: options.SBOMSHA256, Fetch: options.Fetch,
			Quiesce: options.Quiesce, CurrentSHA256: desktopSHA,
			WriteState: options.WriteState,
		},
		incomingDir: incoming, previousDir: previous, statePath: filepath.Join(options.Root, "session.json"),
		state: SessionState{
			SchemaVersion: "mzgui.update.session.v1", Phase: PhaseChecking,
			SelectedKind: "process-set", CurrentSHA256: desktopSHA,
		},
	}, nil
}

func prepareProcessSet(options ProcessSetOptions) ([]preparedComponent, string, error) {
	if len(options.Components) < 2 || len(options.AllowedInstallRoots) == 0 {
		return nil, "", ErrInvalidProcessSet
	}
	seen := make(map[string]LiveComponent, len(options.Components))
	for _, component := range options.Components {
		name := strings.TrimSpace(component.Component)
		if !validProcessComponent(name) || name == "" || !filepath.IsAbs(component.LivePath) {
			return nil, "", ErrInvalidProcessSet
		}
		if _, exists := seen[name]; exists {
			return nil, "", ErrInvalidProcessSet
		}
		if _, err := absRegularFile(component.LivePath); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidSession, err)
		}
		if err := verifyFileHash(component.LivePath, component.CurrentSHA256); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidSession, err)
		}
		seen[name] = component
	}
	if _, ok := seen[ComponentDesktop]; !ok {
		return nil, "", ErrInvalidProcessSet
	}
	if _, ok := seen[ComponentAgentd]; !ok {
		return nil, "", ErrInvalidProcessSet
	}
	if currentHasComponent(options.Current, ComponentHelper) {
		if _, ok := seen[ComponentHelper]; !ok {
			return nil, "", ErrInvalidProcessSet
		}
	}
	order := []string{ComponentDesktop, ComponentAgentd, ComponentHelper}
	prepared := make([]preparedComponent, 0, len(order))
	installRoot := filepath.Dir(seen[ComponentDesktop].LivePath)
	allowedInstallRoot := false
	for _, root := range options.AllowedInstallRoots {
		root = filepath.Clean(strings.TrimSpace(root))
		if filepath.IsAbs(root) && pathWithinRoot(root, installRoot) && requireSafeDirectory(root) == nil {
			allowedInstallRoot = true
			break
		}
	}
	if !allowedInstallRoot || requireSafeDirectory(installRoot) != nil {
		return nil, "", ErrInvalidProcessSet
	}
	for _, name := range order {
		component, ok := seen[name]
		if !ok {
			continue
		}
		artifact, err := options.Manifest.SelectProcessArtifact(options.OS, options.Arch, name)
		if err != nil {
			return nil, "", err
		}
		rel, err := filepath.Rel(installRoot, component.LivePath)
		if err != nil || !pathWithinRoot(installRoot, component.LivePath) || component.LivePath == installRoot {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidProcessSet, err)
		}
		prepared = append(prepared, preparedComponent{
			name: name, livePath: component.LivePath, artifact: artifact, relPath: rel,
		})
	}
	return prepared, installRoot, nil
}

func switchPreparedComponents(items []preparedComponent, replace func(string, string) error) error {
	for _, item := range items {
		if err := replace(item.staged, item.livePath); err != nil {
			return err
		}
	}
	return nil
}

func restorePreparedComponents(items []preparedComponent, replace func(string, string) error) error {
	var first error
	for _, item := range items {
		restorePath := item.staged + ".restore"
		if err := copyExclusive(item.previous, restorePath, maximumArtifactBytes); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := replace(restorePath, item.livePath); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func currentHasComponent(set ProcessSet, name string) bool {
	if set.Desktop.Name == name {
		return true
	}
	for _, sidecar := range set.Sidecars {
		if sidecar.Name == name {
			return true
		}
	}
	return false
}

func processSetFromManifest(manifest Manifest, current ProcessSet) ProcessSet {
	next := ProcessSet{
		Desktop: ProcessArtifact{Name: ComponentDesktop, Version: manifest.Payload.Version, BuildID: manifest.Payload.BuildID},
	}
	for _, sidecar := range current.Sidecars {
		next.Sidecars = append(next.Sidecars, ProcessArtifact{Name: sidecar.Name, Version: manifest.Payload.Version, BuildID: manifest.Payload.BuildID})
	}
	return next
}

func (session *session) writeProcessReceipt(installRoot string, relPaths []string) error {
	path := strings.TrimSpace(session.options.ReceiptPath)
	if path == "" {
		return nil
	}
	receipt, err := BuildReceipt(session.options.Manifest.Payload.Product, session.options.Manifest.Payload.Version, installRoot, relPaths)
	if err != nil {
		return err
	}
	if digest := strings.ToLower(strings.TrimSpace(session.options.SBOMSHA256)); digest != "" {
		receipt.SBOMSHA256 = digest
	}
	if err := WriteReceipt(path, receipt); err != nil {
		return err
	}
	return VerifyReceipt(installRoot, receipt)
}
