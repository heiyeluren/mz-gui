package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	MatrixSchemaVersion    = "mzgui.update.matrix.v1"
	CaseCleanInstall       = "clean_install"
	CaseOverlayUpgrade     = "overlay_upgrade"
	CaseSkipVersionUpgrade = "skip_version_upgrade"
	CaseRollbackOnValidate = "rollback_on_validate"
	matrixProduct          = "mz-gui-desktop"
	matrixChannel          = "stable"
	matrixKeyID            = "matrix-2026"
	matrixDesktopName      = "desktop"
	matrixSidecarName      = "agentd"
	matrixBuildID          = "matrix-local"
)

var ErrInvalidMatrix = errors.New("安装升级矩阵无效")

type MatrixCaseResult struct {
	Name         string `json:"name"`
	Phase        Phase  `json:"phase"`
	SelectedKind string `json:"selected_kind"`
	FallbackUsed bool   `json:"fallback_used"`
	ReceiptOK    bool   `json:"receipt_ok"`
	ProcessSetOK bool   `json:"process_set_ok"`
}

type MatrixResult struct {
	SchemaVersion string             `json:"schema_version"`
	Cases         []MatrixCaseResult `json:"cases"`
}

// RunInstallUpgradeMatrix 本机跑干净安装、覆盖差分升级、跨版本完整包升级和校验失败回滚。
func RunInstallUpgradeMatrix(ctx context.Context, root string) (MatrixResult, error) {
	if ctx == nil || root == "" || !filepath.IsAbs(root) {
		return MatrixResult{}, ErrInvalidMatrix
	}
	if err := ctx.Err(); err != nil {
		return MatrixResult{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return MatrixResult{}, fmt.Errorf("%w: %v", ErrInvalidMatrix, err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return MatrixResult{}, fmt.Errorf("%w: %v", ErrInvalidMatrix, err)
	}
	keys := map[string]ed25519.PublicKey{matrixKeyID: publicKey}
	sbom := hexDigest([]byte("mzgui-sbom-lite"))
	specs := []matrixSpec{
		{
			name: CaseCleanInstall, version: "1.0.0", live: []byte("CLEAN-STUB"),
			full: []byte("CLEAN-FULL-PACKAGE"), wantPhase: PhaseCommitted, wantKind: "full",
		},
		{
			name: CaseOverlayUpgrade, version: "1.1.0", live: []byte("BASE-CONTENT-AAAA"),
			full: []byte("BASE-CONTENT-AAAA-NEW"), includeDelta: true, wantPhase: PhaseCommitted, wantKind: "delta",
		},
		{
			name: CaseSkipVersionUpgrade, version: "1.2.0", live: []byte("CURRENT-LIVE-FILE"),
			full: []byte("SKIP-FULL-PACKAGE"), includeDelta: true, mismatchDelta: true,
			wantPhase: PhaseCommitted, wantKind: "full",
		},
		{
			name: CaseRollbackOnValidate, version: "1.3.0", live: []byte("OLD-LIVE-CONTENT"),
			full: []byte("NEW-LIVE-CONTENT"), includeDelta: true,
			validate:  func(context.Context, string) error { return errors.New("smoke failed") },
			wantPhase: PhaseRolledBack, wantKind: "delta", wantError: ErrUpdateRolledBack,
		},
	}
	result := MatrixResult{SchemaVersion: MatrixSchemaVersion, Cases: make([]MatrixCaseResult, 0, len(specs))}
	for _, spec := range specs {
		caseResult, err := spec.run(ctx, filepath.Join(root, spec.name), privateKey, keys, sbom)
		if err != nil {
			return result, err
		}
		result.Cases = append(result.Cases, caseResult)
	}
	return result, nil
}

type matrixSpec struct {
	name          string
	version       string
	live          []byte
	full          []byte
	includeDelta  bool
	mismatchDelta bool
	validate      func(context.Context, string) error
	wantPhase     Phase
	wantKind      string
	wantError     error
}

func (spec matrixSpec) run(ctx context.Context, root string, privateKey ed25519.PrivateKey, keys map[string]ed25519.PublicKey, sbom string) (MatrixCaseResult, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return MatrixCaseResult{}, fmt.Errorf("%w: %v", ErrInvalidMatrix, err)
	}
	livePath := filepath.Join(root, "app", "mzgui.bin")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o700); err != nil {
		return MatrixCaseResult{}, fmt.Errorf("%w: %v", ErrInvalidMatrix, err)
	}
	if err := os.WriteFile(livePath, spec.live, 0o600); err != nil {
		return MatrixCaseResult{}, fmt.Errorf("%w: %v", ErrInvalidMatrix, err)
	}
	blobs := map[string][]byte{"full": spec.full}
	artifacts := []Artifact{{
		OS: "darwin", Arch: "arm64", Kind: "full", URL: "https://download.example.test/full.zip",
		SHA256: hexDigest(spec.full), Size: int64(len(spec.full)),
	}}
	if spec.includeDelta {
		base := spec.live
		if spec.mismatchDelta {
			base = []byte("OTHER-BASE")
		}
		delta, err := EncodeDelta(base, spec.full)
		if err != nil {
			return MatrixCaseResult{}, err
		}
		blobs["delta"] = delta
		artifacts = append([]Artifact{{
			OS: "darwin", Arch: "arm64", Kind: "delta", URL: "https://download.example.test/delta.zip",
			SHA256: hexDigest(delta), BaseSHA256: hexDigest(base), Size: int64(len(delta)),
		}}, artifacts...)
	}
	manifest, err := signedMatrixManifest(spec.version, artifacts, privateKey, keys)
	if err != nil {
		return MatrixCaseResult{}, err
	}
	receiptPath := filepath.Join(root, "receipt.json")
	state, applyErr := Apply(ctx, SessionOptions{
		Root: filepath.Join(root, "session"), LivePath: livePath, CurrentSHA256: hexDigest(spec.live),
		Manifest: manifest, OS: "darwin", Arch: "arm64", ReceiptPath: receiptPath, SBOMSHA256: sbom,
		Fetch: blobFetch(blobs), Validate: spec.validate,
	})
	if spec.wantError != nil {
		if !errors.Is(applyErr, spec.wantError) {
			return MatrixCaseResult{}, fmt.Errorf("%w: %s 错误=%v", ErrInvalidMatrix, spec.name, applyErr)
		}
	} else if applyErr != nil {
		return MatrixCaseResult{}, fmt.Errorf("%w: %s: %v", ErrInvalidMatrix, spec.name, applyErr)
	}
	if state.Phase != spec.wantPhase || state.SelectedKind != spec.wantKind {
		return MatrixCaseResult{}, fmt.Errorf("%w: %s 状态=%#v", ErrInvalidMatrix, spec.name, state)
	}
	got, err := os.ReadFile(livePath)
	if err != nil {
		return MatrixCaseResult{}, err
	}
	wantLive := spec.full
	if spec.wantPhase == PhaseRolledBack {
		wantLive = spec.live
	}
	if string(got) != string(wantLive) {
		return MatrixCaseResult{}, fmt.Errorf("%w: %s live 内容错误", ErrInvalidMatrix, spec.name)
	}
	result := MatrixCaseResult{
		Name: spec.name, Phase: state.Phase, SelectedKind: state.SelectedKind, FallbackUsed: state.FallbackUsed,
	}
	if spec.wantPhase == PhaseCommitted {
		loaded, loadErr := LoadReceipt(receiptPath)
		result.ReceiptOK = loadErr == nil && loaded.SBOMSHA256 == sbom && VerifyReceipt(filepath.Dir(livePath), loaded) == nil
	} else {
		_, statErr := os.Lstat(receiptPath)
		result.ReceiptOK = errors.Is(statErr, os.ErrNotExist)
	}
	result.ProcessSetOK = CheckProcessSet(ProcessSet{
		Desktop:  ProcessArtifact{Name: matrixDesktopName, Version: spec.version, BuildID: matrixBuildID},
		Sidecars: []ProcessArtifact{{Name: matrixSidecarName, Version: spec.version, BuildID: matrixBuildID}},
	}) == nil
	if !result.ReceiptOK || !result.ProcessSetOK {
		return result, fmt.Errorf("%w: %s 回执或进程集合未通过", ErrInvalidMatrix, spec.name)
	}
	return result, nil
}

func signedMatrixManifest(version string, artifacts []Artifact, privateKey ed25519.PrivateKey, keys map[string]ed25519.PublicKey) (Manifest, error) {
	payload := Payload{
		SchemaVersion: 1, Product: matrixProduct, Channel: matrixChannel, Version: version, BuildID: matrixBuildID,
		PublishedAt: "2026-08-16T08:00:00Z", ExpiresAt: "2026-08-17T08:00:00Z", Artifacts: artifacts,
	}
	data, err := Sign(payload, matrixKeyID, privateKey)
	if err != nil {
		return Manifest{}, err
	}
	return ParseAndVerify(data, VerifyPolicy{
		Product: matrixProduct, Channel: matrixChannel, Keys: keys,
		Now: func() time.Time { return time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC) },
	})
}

func blobFetch(blobs map[string][]byte) func(context.Context, Artifact, string) error {
	return func(_ context.Context, artifact Artifact, dest string) error {
		data, ok := blobs[artifact.Kind]
		if !ok {
			return fmt.Errorf("缺少制品 %s", artifact.Kind)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o600)
	}
}

func hexDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
