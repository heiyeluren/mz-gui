package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Phase string

const (
	PhaseChecking       Phase = "checking"
	PhaseDownloading    Phase = "downloading"
	PhaseVerified       Phase = "verified"
	PhaseStaged         Phase = "staged"
	PhaseQuiescing      Phase = "quiescing"
	PhaseSwitching      Phase = "switching"
	PhaseValidating     Phase = "validating"
	PhaseCommitted      Phase = "committed"
	PhaseRolledBack     Phase = "rolled_back"
	PhaseRepairRequired Phase = "repair_required"
)

var (
	ErrInvalidSession       = errors.New("更新会话无效")
	ErrUpdateRolledBack     = errors.New("更新已回滚")
	ErrUpdateRepairRequired = errors.New("更新需要人工修复")
)

type UpdatePlan struct {
	Preferred Artifact
	Fallback  Artifact
}

type SessionOptions struct {
	Root          string
	LivePath      string
	CurrentSHA256 string
	Manifest      Manifest
	OS            string
	Arch          string
	ReceiptPath   string
	SBOMSHA256    string
	Fetch         func(context.Context, Artifact, string) error
	Quiesce       func(context.Context) error
	Validate      func(context.Context, string) error
	WriteState    func(path string, data []byte) error
}

type SessionState struct {
	SchemaVersion string `json:"schema_version"`
	Phase         Phase  `json:"phase"`
	SelectedKind  string `json:"selected_kind"`
	FallbackUsed  bool   `json:"fallback_used"`
	CurrentSHA256 string `json:"current_sha256"`
	TargetSHA256  string `json:"target_sha256"`
	Reason        string `json:"reason,omitempty"`
}

// PlanUpdate 优先选择基础摘要匹配的差分包，否则回退完整包。
func (manifest Manifest) PlanUpdate(goos, goarch, currentSHA256 string) (UpdatePlan, error) {
	currentSHA256 = strings.ToLower(strings.TrimSpace(currentSHA256))
	delta, deltaErr := manifest.SelectArtifactKind(goos, goarch, "delta")
	full, fullErr := manifest.SelectArtifactKind(goos, goarch, "full")
	if deltaErr == nil && fullErr == nil && sha256Pattern.MatchString(currentSHA256) && strings.ToLower(delta.BaseSHA256) == currentSHA256 {
		return UpdatePlan{Preferred: delta, Fallback: full}, nil
	}
	if fullErr == nil {
		return UpdatePlan{Preferred: full}, nil
	}
	return UpdatePlan{}, ErrArtifactNotFound
}

// Apply 执行 checking→committed 状态机；切换失败且无法恢复旧制品时进入 repair_required。
func Apply(ctx context.Context, options SessionOptions) (SessionState, error) {
	session, err := newSession(ctx, options)
	if err != nil {
		return SessionState{}, err
	}
	plan, err := options.Manifest.PlanUpdate(options.OS, options.Arch, options.CurrentSHA256)
	if err != nil {
		return session.failBeforeSwitch("artifact_not_found", err)
	}
	session.state.SelectedKind = plan.Preferred.Kind
	session.state.TargetSHA256 = strings.ToLower(plan.Preferred.SHA256)
	if plan.Preferred.Kind == "delta" {
		session.state.TargetSHA256 = strings.ToLower(plan.Fallback.SHA256)
	}
	if err := session.persist(PhaseChecking, ""); err != nil {
		return session.state, err
	}

	downloadPath := filepath.Join(session.incomingDir, "download.bin")
	stagedPath := filepath.Join(session.incomingDir, "staged.bin")
	previousPath := filepath.Join(session.previousDir, "live.bin")
	selected := plan.Preferred
	if err := session.persist(PhaseDownloading, ""); err != nil {
		return session.state, err
	}
	if err := fetchAndVerify(ctx, options.Fetch, selected, downloadPath); err != nil {
		if plan.Fallback.SHA256 == "" {
			return session.failBeforeSwitch("download_failed", err)
		}
		selected = plan.Fallback
		session.state.FallbackUsed = true
		session.state.SelectedKind = selected.Kind
		session.state.TargetSHA256 = strings.ToLower(selected.SHA256)
		if err := fetchAndVerify(ctx, options.Fetch, selected, downloadPath); err != nil {
			return session.failBeforeSwitch("download_failed", err)
		}
	}
	if err := session.persist(PhaseVerified, ""); err != nil {
		return session.state, err
	}

	if err := session.stage(ctx, selected, downloadPath, stagedPath, plan); err != nil {
		return session.failBeforeSwitch(session.state.Reason, err)
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
	if err := copyExclusive(options.LivePath, previousPath, maximumArtifactBytes); err != nil {
		return session.failBeforeSwitch("backup_failed", err)
	}
	if err := session.persist(PhaseSwitching, ""); err != nil {
		return session.state, err
	}
	if err := replaceFile(stagedPath, options.LivePath); err != nil {
		if restoreErr := restorePreviousArtifact(previousPath, options.LivePath, filepath.Join(session.incomingDir, "restore-live.bin")); restoreErr != nil {
			return session.failRepair("switch_failed")
		}
		return session.failBeforeSwitch("switch_failed", err)
	}
	if err := session.persist(PhaseValidating, ""); err != nil {
		if restoreErr := restorePreviousArtifact(previousPath, options.LivePath, filepath.Join(session.incomingDir, "restore-live.bin")); restoreErr != nil {
			return session.failRepair("journal_failed")
		}
		return session.failRolledBack("journal_failed")
	}
	if options.Validate != nil {
		if err := options.Validate(ctx, options.LivePath); err != nil {
			if restoreErr := restorePreviousArtifact(previousPath, options.LivePath, filepath.Join(session.incomingDir, "restore-live.bin")); restoreErr != nil {
				return session.failRepair("validate_failed")
			}
			return session.failRolledBack("validate_failed")
		}
	}
	receiptBefore, err := captureReceiptSnapshot(options.ReceiptPath)
	if err != nil {
		if restoreErr := restorePreviousArtifact(previousPath, options.LivePath, filepath.Join(session.incomingDir, "restore-live.bin")); restoreErr != nil {
			return session.failRepair("receipt_backup_failed")
		}
		return session.failRolledBack("receipt_backup_failed")
	}
	if err := session.writeReceipt(); err != nil {
		restoreErr := restorePreviousArtifact(previousPath, options.LivePath, filepath.Join(session.incomingDir, "restore-live.bin"))
		receiptErr := restoreReceiptSnapshot(receiptBefore)
		if restoreErr != nil || receiptErr != nil {
			return session.failRepair("receipt_failed")
		}
		return session.failRolledBack("receipt_failed")
	}
	if err := session.persist(PhaseCommitted, ""); err != nil {
		restoreErr := restorePreviousArtifact(previousPath, options.LivePath, filepath.Join(session.incomingDir, "restore-live.bin"))
		receiptErr := restoreReceiptSnapshot(receiptBefore)
		if restoreErr != nil || receiptErr != nil {
			return session.failRepair("commit_journal_failed")
		}
		return session.failRolledBack("commit_journal_failed")
	}
	return session.state, nil
}

type session struct {
	options     SessionOptions
	incomingDir string
	previousDir string
	statePath   string
	state       SessionState
}

func newSession(ctx context.Context, options SessionOptions) (*session, error) {
	if ctx == nil || options.Fetch == nil || options.Root == "" || !filepath.IsAbs(options.Root) ||
		options.LivePath == "" || !filepath.IsAbs(options.LivePath) || !sha256Pattern.MatchString(strings.ToLower(options.CurrentSHA256)) {
		return nil, ErrInvalidSession
	}
	if !options.Manifest.Verified() {
		return nil, ErrUntrustedManifest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := absRegularFile(options.LivePath); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	incoming := filepath.Join(options.Root, "incoming")
	previous := filepath.Join(options.Root, "previous")
	for _, directory := range []string{incoming, previous} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSession, err)
		}
	}
	return &session{
		options:     options,
		incomingDir: incoming,
		previousDir: previous,
		statePath:   filepath.Join(options.Root, "session.json"),
		state: SessionState{
			SchemaVersion: "mzgui.update.session.v1",
			Phase:         PhaseChecking,
			CurrentSHA256: strings.ToLower(options.CurrentSHA256),
		},
	}, nil
}

func (session *session) stage(ctx context.Context, selected Artifact, downloadPath, stagedPath string, plan UpdatePlan) error {
	if selected.Kind != "delta" {
		return copyExclusive(downloadPath, stagedPath, maximumArtifactBytes)
	}
	if err := ApplyDelta(session.options.LivePath, downloadPath, stagedPath); err == nil {
		if verifyErr := verifyArtifactFile(stagedPath, plan.Fallback); verifyErr == nil {
			return nil
		}
	} else if plan.Fallback.SHA256 == "" {
		session.state.Reason = "delta_apply_failed"
		return err
	}
	session.state.FallbackUsed = true
	session.state.SelectedKind = plan.Fallback.Kind
	session.state.TargetSHA256 = strings.ToLower(plan.Fallback.SHA256)
	fallbackPath := filepath.Join(session.incomingDir, "fallback.bin")
	if err := fetchAndVerify(ctx, session.options.Fetch, plan.Fallback, fallbackPath); err != nil {
		session.state.Reason = "download_failed"
		return err
	}
	if err := copyExclusive(fallbackPath, stagedPath, maximumArtifactBytes); err != nil {
		session.state.Reason = "stage_failed"
		return err
	}
	return nil
}

func (session *session) writeReceipt() error {
	path := strings.TrimSpace(session.options.ReceiptPath)
	if path == "" {
		return nil
	}
	installRoot := filepath.Dir(session.options.LivePath)
	rel, err := filepath.Rel(installRoot, session.options.LivePath)
	if err != nil {
		return err
	}
	receipt, err := BuildReceipt(session.options.Manifest.Payload.Product, session.options.Manifest.Payload.Version, installRoot, []string{rel})
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

func (session *session) persist(phase Phase, reason string) error {
	session.state.Phase = phase
	if reason != "" {
		session.state.Reason = reason
	}
	data, err := json.MarshalIndent(session.state, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	writer := session.options.WriteState
	if writer == nil {
		writer = writeSessionStateAtomic
	}
	return writer(session.statePath, data)
}

func (session *session) failBeforeSwitch(reason string, cause error) (SessionState, error) {
	_ = session.persist(PhaseRolledBack, reason)
	if cause == nil {
		cause = ErrUpdateRolledBack
	}
	return session.state, fmt.Errorf("%w: %v", ErrUpdateRolledBack, cause)
}

func (session *session) failRolledBack(reason string) (SessionState, error) {
	_ = session.persist(PhaseRolledBack, reason)
	return session.state, ErrUpdateRolledBack
}

func (session *session) failRepair(reason string) (SessionState, error) {
	_ = session.persist(PhaseRepairRequired, reason)
	return session.state, ErrUpdateRepairRequired
}

func fetchAndVerify(ctx context.Context, fetch func(context.Context, Artifact, string) error, artifact Artifact, dest string) error {
	_ = os.Remove(dest)
	if err := fetch(ctx, artifact, dest); err != nil {
		return err
	}
	return verifyArtifactFile(dest, artifact)
}

func verifyArtifactFile(path string, artifact Artifact) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != artifact.Size {
		return fmt.Errorf("%w: 制品类型或大小不匹配", ErrInvalidSession)
	}
	return verifyFileHash(path, artifact.SHA256)
}

func writeSessionStateAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := requireSafeDirectory(directory); err != nil {
		return fmt.Errorf("%w: 更新会话目录不安全", ErrInvalidSession)
	}
	temporary, err := os.CreateTemp(directory, ".session-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSession, err)
	}
	return nil
}

type receiptSnapshot struct {
	path    string
	data    []byte
	existed bool
}

func captureReceiptSnapshot(path string) (receiptSnapshot, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return receiptSnapshot{}, nil
	}
	if !filepath.IsAbs(path) || requireSafeDirectory(filepath.Dir(path)) != nil {
		return receiptSnapshot{}, ErrInvalidReceipt
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return receiptSnapshot{path: path}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > maximumSessionStateBytes {
		return receiptSnapshot{}, ErrInvalidReceipt
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return receiptSnapshot{}, ErrInvalidReceipt
	}
	return receiptSnapshot{path: path, data: data, existed: true}, nil
}

func restoreReceiptSnapshot(snapshot receiptSnapshot) error {
	if snapshot.path == "" {
		return nil
	}
	if !snapshot.existed {
		if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeSessionStateAtomic(snapshot.path, snapshot.data)
}

func restorePreviousArtifact(previousPath, livePath, restorePath string) error {
	if err := copyExclusive(previousPath, restorePath, maximumArtifactBytes); err != nil {
		return err
	}
	return replaceFile(restorePath, livePath)
}

func verifyFileHash(path, expected string) error {
	digest, err := hashFile(path)
	if err != nil {
		return err
	}
	if digest != strings.ToLower(strings.TrimSpace(expected)) {
		return fmt.Errorf("%w: 制品摘要不匹配", ErrInvalidSession)
	}
	return nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开制品计算摘要: %w", err)
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", fmt.Errorf("计算制品摘要: %w", err)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func copyExclusive(source, dest string, maxBytes int64) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if _, err := os.Lstat(dest); err == nil {
		if err := os.Remove(dest); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer output.Close()
	written, err := io.Copy(output, io.LimitReader(input, maxBytes+1))
	if err != nil {
		return err
	}
	if written > maxBytes {
		return fmt.Errorf("制品超过容量上限")
	}
	return output.Sync()
}

func replaceFile(source, dest string) error {
	if err := os.Rename(source, dest); err == nil {
		return nil
	}
	backup := dest + ".switching"
	_ = os.Remove(backup)
	if err := os.Rename(dest, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(source, dest); err != nil {
		if _, statErr := os.Lstat(backup); statErr == nil {
			_ = os.Rename(backup, dest)
		}
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func absRegularFile(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("路径必须是绝对路径")
	}
	clean := filepath.Clean(path)
	info, err := os.Lstat(clean)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("路径必须是普通文件")
	}
	return clean, nil
}
