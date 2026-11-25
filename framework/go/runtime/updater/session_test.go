package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyPrefersMatchingDeltaAndCommitsNewLiveArtifact(t *testing.T) {
	base := []byte("BASE-CONTENT-AAAA")
	target := []byte("BASE-CONTENT-AAAA-NEW-TAIL")
	full := append([]byte(nil), target...)
	delta, err := EncodeDelta(base, target)
	if err != nil {
		t.Fatal(err)
	}
	env := newUpdateEnv(t, base, hexSHA256(base), delta, full)
	state, err := Apply(context.Background(), SessionOptions{
		Root: env.root, LivePath: env.livePath, CurrentSHA256: hexSHA256(base),
		Manifest: env.manifest, OS: "darwin", Arch: "arm64",
		Fetch:    env.fetch(map[string][]byte{"delta": delta, "full": full}),
		Validate: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("差分更新应提交: %v", err)
	}
	if state.Phase != PhaseCommitted || state.SelectedKind != "delta" || state.FallbackUsed {
		t.Fatalf("提交状态错误: %#v", state)
	}
	got, err := os.ReadFile(env.livePath)
	if err != nil || !bytes.Equal(got, target) {
		t.Fatalf("live 未切换到目标制品: got=%q err=%v", got, err)
	}
}

func TestApplyFallsBackToFullWhenDeltaBaseMismatchesOrApplyFails(t *testing.T) {
	live := []byte("CURRENT-LIVE-FILE")
	target := []byte("FULL-PACKAGE-CONTENT-XXXX")
	badDelta := []byte("not-a-delta")
	env := newUpdateEnv(t, live, hexSHA256([]byte("other-base")), badDelta, target)
	state, err := Apply(context.Background(), SessionOptions{
		Root: env.root, LivePath: env.livePath, CurrentSHA256: hexSHA256(live),
		Manifest: env.manifest, OS: "darwin", Arch: "arm64",
		Fetch:    env.fetch(map[string][]byte{"delta": badDelta, "full": target}),
		Validate: func(context.Context, string) error { return nil },
	})
	if err != nil || state.Phase != PhaseCommitted || state.SelectedKind != "full" {
		t.Fatalf("基础不匹配应选择完整包: state=%#v err=%v", state, err)
	}
	got, _ := os.ReadFile(env.livePath)
	if !bytes.Equal(got, target) {
		t.Fatalf("回退完整包后 live 错误: %q", got)
	}

	env = newUpdateEnv(t, live, hexSHA256(live), badDelta, target)
	state, err = Apply(context.Background(), SessionOptions{
		Root: env.root, LivePath: env.livePath, CurrentSHA256: hexSHA256(live),
		Manifest: env.manifest, OS: "darwin", Arch: "arm64",
		Fetch:    env.fetch(map[string][]byte{"delta": badDelta, "full": target}),
		Validate: func(context.Context, string) error { return nil },
	})
	if err != nil || !state.FallbackUsed || state.Phase != PhaseCommitted {
		t.Fatalf("差分应用失败应回退完整包: state=%#v err=%v", state, err)
	}
}

func TestApplyRollsBackWhenValidationFailsAndKeepsOldLive(t *testing.T) {
	base := []byte("OLD-LIVE-CONTENT")
	target := []byte("NEW-LIVE-CONTENT")
	delta := mustDelta(t, base, target)
	env := newUpdateEnv(t, base, hexSHA256(base), delta, target)
	state, err := Apply(context.Background(), SessionOptions{
		Root: env.root, LivePath: env.livePath, CurrentSHA256: hexSHA256(base),
		Manifest: env.manifest, OS: "darwin", Arch: "arm64",
		Fetch:    env.fetch(map[string][]byte{"delta": delta, "full": target}),
		Validate: func(context.Context, string) error { return errors.New("smoke failed") },
	})
	if !errors.Is(err, ErrUpdateRolledBack) || state.Phase != PhaseRolledBack {
		t.Fatalf("校验失败应回滚: state=%#v err=%v", state, err)
	}
	got, err := os.ReadFile(env.livePath)
	if err != nil || !bytes.Equal(got, base) {
		t.Fatalf("回滚后应保留旧制品: got=%q err=%v", got, err)
	}
}

func TestApplyWritesReceiptAfterCommitAndKeepsOldLiveWithoutReceiptOnRollback(t *testing.T) {
	base := []byte("OLD-LIVE-CONTENT")
	target := []byte("NEW-LIVE-CONTENT")
	delta := mustDelta(t, base, target)
	env := newUpdateEnv(t, base, hexSHA256(base), delta, target)
	receiptPath := filepath.Join(env.root, "receipt.json")
	sbom := hexSHA256([]byte("mzgui-sbom-lite"))
	state, err := Apply(context.Background(), SessionOptions{
		Root: env.root, LivePath: env.livePath, CurrentSHA256: hexSHA256(base),
		Manifest: env.manifest, OS: "darwin", Arch: "arm64", ReceiptPath: receiptPath, SBOMSHA256: sbom,
		Fetch:    env.fetch(map[string][]byte{"delta": delta, "full": target}),
		Validate: func(context.Context, string) error { return nil },
	})
	if err != nil || state.Phase != PhaseCommitted {
		t.Fatalf("提交应写入回执: state=%#v err=%v", state, err)
	}
	loaded, err := LoadReceipt(receiptPath)
	if err != nil || loaded.SBOMSHA256 != sbom {
		t.Fatalf("提交回执无效: %#v err=%v", loaded, err)
	}
	if err := VerifyReceipt(filepath.Dir(env.livePath), loaded); err != nil {
		t.Fatal(err)
	}

	env = newUpdateEnv(t, base, hexSHA256(base), delta, target)
	receiptPath = filepath.Join(env.root, "receipt.json")
	_, err = Apply(context.Background(), SessionOptions{
		Root: env.root, LivePath: env.livePath, CurrentSHA256: hexSHA256(base),
		Manifest: env.manifest, OS: "darwin", Arch: "arm64", ReceiptPath: receiptPath,
		Fetch:    env.fetch(map[string][]byte{"delta": delta, "full": target}),
		Validate: func(context.Context, string) error { return errors.New("smoke failed") },
	})
	if !errors.Is(err, ErrUpdateRolledBack) {
		t.Fatalf("校验失败应回滚: %v", err)
	}
	if _, err := os.Lstat(receiptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("回滚不得留下新回执: %v", err)
	}
}

func TestApplyRemovesNewReceiptWhenCommitJournalWriteFails(t *testing.T) {
	base := []byte("OLD-LIVE-CONTENT")
	target := []byte("NEW-LIVE-CONTENT")
	env := newUpdateEnv(t, base, hexSHA256(base), mustDelta(t, base, target), target)
	receiptPath := filepath.Join(env.root, "receipt.json")
	state, err := Apply(context.Background(), SessionOptions{
		Root: env.root, LivePath: env.livePath, CurrentSHA256: hexSHA256(base),
		Manifest: env.manifest, OS: "darwin", Arch: "arm64", ReceiptPath: receiptPath,
		Fetch: env.fetch(map[string][]byte{"delta": mustDelta(t, base, target), "full": target}),
		WriteState: func(path string, data []byte) error {
			var state SessionState
			if json.Unmarshal(data, &state) == nil && state.Phase == PhaseCommitted {
				return errors.New("disk full")
			}
			return writeSessionStateAtomic(path, data)
		},
	})
	if !errors.Is(err, ErrUpdateRolledBack) || state.Reason != "commit_journal_failed" {
		t.Fatalf("提交 journal 写失败必须回滚: state=%#v err=%v", state, err)
	}
	if got := mustRead(t, env.livePath); !bytes.Equal(got, base) {
		t.Fatalf("提交 journal 写失败后应恢复旧制品: %q", got)
	}
	if _, statErr := os.Lstat(receiptPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("提交 journal 写失败不得保留新回执: %v", statErr)
	}
}

func TestApplyLeavesLiveUntouchedWhenFullFetchFails(t *testing.T) {
	live := []byte("UNTOUCHED-LIVE")
	target := []byte("target")
	env := newUpdateEnv(t, live, hexSHA256(live), target, target)
	state, err := Apply(context.Background(), SessionOptions{
		Root: env.root, LivePath: env.livePath, CurrentSHA256: hexSHA256(live),
		Manifest: env.manifest, OS: "darwin", Arch: "arm64",
		Fetch: func(context.Context, Artifact, string) error { return errors.New("offline") },
	})
	if !errors.Is(err, ErrUpdateRolledBack) || state.Phase != PhaseRolledBack {
		t.Fatalf("下载失败应中止且不切换: state=%#v err=%v", state, err)
	}
	got, _ := os.ReadFile(env.livePath)
	if !bytes.Equal(got, live) {
		t.Fatalf("下载失败不得改写 live: %q", got)
	}
}

func TestApplyRejectsManifestThatWasNotCryptographicallyVerified(t *testing.T) {
	root := t.TempDir()
	livePath := filepath.Join(root, "app.bin")
	live := []byte("CURRENT")
	if err := os.WriteFile(livePath, live, 0o600); err != nil {
		t.Fatal(err)
	}
	forged := Manifest{Payload: Payload{
		SchemaVersion: 1, Product: "mz-gui-desktop", Channel: "stable", Version: "9.9.9",
		Artifacts: []Artifact{{OS: "darwin", Arch: "arm64", Kind: "full", URL: "https://evil.invalid/update.zip", SHA256: hexSHA256([]byte("EVIL")), Size: 4}},
	}}
	state, err := Apply(context.Background(), SessionOptions{
		Root: filepath.Join(root, "session"), LivePath: livePath, CurrentSHA256: hexSHA256(live),
		Manifest: forged, OS: "darwin", Arch: "arm64",
		Fetch: func(context.Context, Artifact, string) error { return nil },
	})
	if !errors.Is(err, ErrUntrustedManifest) || state.Phase != "" {
		t.Fatalf("未经验签的内存 Manifest 必须在写文件前拒绝: state=%#v err=%v", state, err)
	}
}

func TestApplyRejectsFetcherSymlinkAndWrongSizeBeforeSwitch(t *testing.T) {
	live := []byte("CURRENT-LIVE")
	target := []byte("NEXT-LIVE")
	env := newUpdateEnv(t, live, hexSHA256([]byte("other-base")), target, target)
	state, err := Apply(context.Background(), SessionOptions{
		Root: env.root, LivePath: env.livePath, CurrentSHA256: hexSHA256(live),
		Manifest: env.manifest, OS: "darwin", Arch: "arm64",
		Fetch: func(_ context.Context, _ Artifact, dest string) error {
			outside := filepath.Join(t.TempDir(), "outside.bin")
			if err := os.WriteFile(outside, target, 0o600); err != nil {
				return err
			}
			return os.Symlink(outside, dest)
		},
	})
	if !errors.Is(err, ErrUpdateRolledBack) || state.Phase != PhaseRolledBack {
		t.Fatalf("下载器返回符号链接必须在切换前拒绝: state=%#v err=%v", state, err)
	}
	if got := mustRead(t, env.livePath); !bytes.Equal(got, live) {
		t.Fatalf("拒绝不安全下载后 live 不得变化: %q", got)
	}
}

type updateEnv struct {
	root     string
	livePath string
	manifest Manifest
}

func newUpdateEnv(t *testing.T, live []byte, deltaBase string, delta, full []byte) updateEnv {
	t.Helper()
	root := t.TempDir()
	livePath := filepath.Join(root, "app", "mzgui.bin")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, live, 0o600); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := Payload{
		SchemaVersion: 1, Product: "mz-gui-desktop", Channel: "stable", Version: "1.5.0",
		PublishedAt: "2026-08-16T08:00:00Z", ExpiresAt: "2026-08-17T08:00:00Z",
		Artifacts: []Artifact{
			{OS: "darwin", Arch: "arm64", Kind: "delta", URL: "https://download.example.test/delta.zip", SHA256: hexSHA256(delta), BaseSHA256: deltaBase, Size: int64(len(delta))},
			{OS: "darwin", Arch: "arm64", Kind: "full", URL: "https://download.example.test/full.zip", SHA256: hexSHA256(full), Size: int64(len(full))},
		},
	}
	manifest, err := ParseAndVerify(signedManifest(t, "release-2026", privateKey, payload), VerifyPolicy{
		Product: "mz-gui-desktop", Channel: "stable",
		Keys: map[string]ed25519.PublicKey{"release-2026": publicKey},
		Now:  func() time.Time { return time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return updateEnv{root: filepath.Join(root, "session"), livePath: livePath, manifest: manifest}
}

func (env updateEnv) fetch(blobs map[string][]byte) func(context.Context, Artifact, string) error {
	return func(_ context.Context, artifact Artifact, dest string) error {
		data, ok := blobs[artifact.Kind]
		if !ok {
			return errors.New("missing artifact")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o600)
	}
}

func mustDelta(t *testing.T, base, target []byte) []byte {
	t.Helper()
	data, err := EncodeDelta(base, target)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
