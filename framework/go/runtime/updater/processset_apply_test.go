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

func TestApplyProcessSetSwitchesDesktopAgentdAndHelperTogether(t *testing.T) {
	env := newProcessSetEnv(t, true)
	state, err := ApplyProcessSet(context.Background(), env.options(env.fetch(), nil, nil))
	if err != nil || state.Phase != PhaseCommitted || state.SelectedKind != "process-set" {
		t.Fatalf("同清单进程集合应一次提交: state=%#v err=%v", state, err)
	}
	env.assertLive(t, env.next)
	receipt, err := LoadReceipt(env.receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReceipt(env.installRoot, receipt); err != nil {
		t.Fatal(err)
	}
	if err := RequireStartableProcessSet(ProcessSet{
		Desktop: ProcessArtifact{Name: ComponentDesktop, Version: env.manifest.Payload.Version, BuildID: env.manifest.Payload.BuildID},
		Sidecars: []ProcessArtifact{
			{Name: ComponentAgentd, Version: env.manifest.Payload.Version, BuildID: env.manifest.Payload.BuildID},
			{Name: ComponentHelper, Version: env.manifest.Payload.Version, BuildID: env.manifest.Payload.BuildID},
		},
	}); err != nil {
		t.Fatalf("提交后进程集合必须可启动: %v", err)
	}
}

func TestApplyProcessSetRollsBackAllWhenValidationFails(t *testing.T) {
	env := newProcessSetEnv(t, true)
	state, err := ApplyProcessSet(context.Background(), env.options(env.fetch(), nil, func(context.Context, map[string]string) error {
		return errors.New("smoke failed")
	}))
	if !errors.Is(err, ErrUpdateRolledBack) || state.Phase != PhaseRolledBack {
		t.Fatalf("校验失败应整组回滚: state=%#v err=%v", state, err)
	}
	env.assertLive(t, env.current)
	if _, err := os.Lstat(env.receiptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("回滚不得留下新回执: %v", err)
	}
}

func TestApplyProcessSetRestoresOldSetWhenSwitchFailsMidway(t *testing.T) {
	env := newProcessSetEnv(t, false)
	calls := 0
	state, err := ApplyProcessSet(context.Background(), env.options(env.fetch(), func(source, dest string) error {
		calls++
		if calls == 2 {
			return errors.New("agentd switch failed")
		}
		return replaceFile(source, dest)
	}, nil))
	if !errors.Is(err, ErrUpdateRolledBack) || state.Phase != PhaseRolledBack {
		t.Fatalf("中途切换失败应恢复旧集合: state=%#v err=%v", state, err)
	}
	env.assertLive(t, env.current)
	if mixed := bytes.Contains(mustRead(t, env.live[ComponentDesktop]), []byte("NEW-")) && bytes.Contains(mustRead(t, env.live[ComponentAgentd]), []byte("OLD-")); mixed {
		t.Fatal("不得留下新 desktop 配旧 agentd")
	}
}

func TestApplyProcessSetRejectsMixedCurrentSetWithoutTouchingLive(t *testing.T) {
	env := newProcessSetEnv(t, false)
	options := env.options(env.fetch(), nil, nil)
	options.Current.Sidecars[0].Version = "1.4.1"
	state, err := ApplyProcessSet(context.Background(), options)
	if !errors.Is(err, ErrVersionMismatch) || state.Phase != "" {
		t.Fatalf("混跑当前集合必须拒绝启动: state=%#v err=%v", state, err)
	}
	env.assertLive(t, env.current)
}

func TestApplyProcessSetRejectsManifestMissingAgentd(t *testing.T) {
	env := newProcessSetEnv(t, false)
	filtered := env.manifest.Payload.Artifacts[:0]
	for _, artifact := range env.manifest.Payload.Artifacts {
		if artifact.Component != ComponentAgentd {
			filtered = append(filtered, artifact)
		}
	}
	env.manifest.Payload.Artifacts = filtered
	state, err := ApplyProcessSet(context.Background(), env.options(env.fetch(), nil, nil))
	if !errors.Is(err, ErrUntrustedManifest) {
		t.Fatalf("验签后篡改 agentd 制品必须拒绝: state=%#v err=%v", state, err)
	}
	env.assertLive(t, env.current)
}

func TestApplyProcessSetRejectsComponentOutsideAllowedInstallRoot(t *testing.T) {
	env := newProcessSetEnv(t, false)
	options := env.options(env.fetch(), nil, nil)
	outside := filepath.Join(t.TempDir(), "agentd.bin")
	if err := os.WriteFile(outside, env.current[ComponentAgentd], 0o600); err != nil {
		t.Fatal(err)
	}
	for index := range options.Components {
		if options.Components[index].Component == ComponentAgentd {
			options.Components[index].LivePath = outside
		}
	}
	state, err := ApplyProcessSet(context.Background(), options)
	if !errors.Is(err, ErrInvalidProcessSet) || state.Phase != "" {
		t.Fatalf("进程路径越出安装根必须拒绝: state=%#v err=%v", state, err)
	}
	env.assertLive(t, env.current)
}

func TestRecoverProcessSetRestoresPreviousFilesAfterInterruptedSwitch(t *testing.T) {
	env := newProcessSetEnv(t, false)
	options := env.options(env.fetch(), nil, nil)
	if err := os.MkdirAll(filepath.Join(env.root, "previous"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, component := range options.Components {
		previous := filepath.Join(env.root, "previous", component.Component+".bin")
		if err := os.WriteFile(previous, env.current[component.Component], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(env.live[ComponentDesktop], env.next[ComponentDesktop], 0o600); err != nil {
		t.Fatal(err)
	}
	stateData, err := json.Marshal(SessionState{SchemaVersion: "mzgui.update.session.v1", Phase: PhaseSwitching, SelectedKind: "process-set"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.root, "session.json"), stateData, 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := RecoverProcessSet(context.Background(), ProcessSetRecoveryOptions{
		Root: env.root, Components: options.Components, AllowedInstallRoots: options.AllowedInstallRoots,
	})
	if err != nil || state.Phase != PhaseRolledBack || state.Reason != "interrupted_switch_recovered" {
		t.Fatalf("中断切换应恢复旧进程集合: state=%#v err=%v", state, err)
	}
	env.assertLive(t, env.current)
}

func TestApplyProcessSetRollsBackWhenPostSwitchJournalWriteFails(t *testing.T) {
	env := newProcessSetEnv(t, false)
	options := env.options(env.fetch(), nil, nil)
	options.WriteState = func(path string, data []byte) error {
		var state SessionState
		if err := json.Unmarshal(data, &state); err != nil {
			return err
		}
		if state.Phase == PhaseValidating {
			return errors.New("disk full")
		}
		return writeSessionStateAtomic(path, data)
	}
	state, err := ApplyProcessSet(context.Background(), options)
	if !errors.Is(err, ErrUpdateRolledBack) || state.Phase != PhaseRolledBack || state.Reason != "journal_failed" {
		t.Fatalf("切换后 journal 写失败必须回滚: state=%#v err=%v", state, err)
	}
	env.assertLive(t, env.current)
}

func TestApplyProcessSetRemovesNewReceiptWhenCommitJournalWriteFails(t *testing.T) {
	env := newProcessSetEnv(t, false)
	options := env.options(env.fetch(), nil, nil)
	options.WriteState = func(path string, data []byte) error {
		var state SessionState
		if json.Unmarshal(data, &state) == nil && state.Phase == PhaseCommitted {
			return errors.New("disk full")
		}
		return writeSessionStateAtomic(path, data)
	}
	state, err := ApplyProcessSet(context.Background(), options)
	if !errors.Is(err, ErrUpdateRolledBack) || state.Reason != "commit_journal_failed" {
		t.Fatalf("提交 journal 写失败必须整组回滚: state=%#v err=%v", state, err)
	}
	env.assertLive(t, env.current)
	if _, statErr := os.Lstat(env.receiptPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("提交 journal 写失败不得保留新回执: %v", statErr)
	}
}

func TestSelectArtifactKindIgnoresComponentScopedFullPackages(t *testing.T) {
	generic := hexSHA256([]byte("generic-full"))
	manifest := Manifest{Payload: Payload{Artifacts: []Artifact{
		{OS: "darwin", Arch: "arm64", Kind: "full", Component: ComponentDesktop, SHA256: hexSHA256([]byte("desktop"))},
		{OS: "darwin", Arch: "arm64", Kind: "full", SHA256: generic},
	}}}
	selected, err := manifest.SelectArtifactKind("darwin", "arm64", "full")
	if err != nil || selected.SHA256 != generic || selected.Component != "" {
		t.Fatalf("通用完整包不应被进程制品遮蔽: %#v err=%v", selected, err)
	}
	process, err := manifest.SelectProcessArtifact("darwin", "arm64", ComponentDesktop)
	if err != nil || process.Component != ComponentDesktop {
		t.Fatalf("应按组件选择进程制品: %#v err=%v", process, err)
	}
}

type processSetEnv struct {
	root        string
	installRoot string
	receiptPath string
	live        map[string]string
	current     map[string][]byte
	next        map[string][]byte
	manifest    Manifest
}

func newProcessSetEnv(t *testing.T, withHelper bool) processSetEnv {
	t.Helper()
	root := t.TempDir()
	installRoot := filepath.Join(root, "app")
	if err := os.MkdirAll(installRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	current := map[string][]byte{
		ComponentDesktop: []byte("OLD-desktop"),
		ComponentAgentd:  []byte("OLD-agentd"),
	}
	next := map[string][]byte{
		ComponentDesktop: []byte("NEW-desktop"),
		ComponentAgentd:  []byte("NEW-agentd"),
	}
	if withHelper {
		current[ComponentHelper] = []byte("OLD-helper")
		next[ComponentHelper] = []byte("NEW-helper")
	}
	live := map[string]string{}
	for name, payload := range current {
		path := filepath.Join(installRoot, name+".bin")
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		live[name] = path
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := make([]Artifact, 0, len(next))
	for name, payload := range next {
		artifacts = append(artifacts, Artifact{
			OS: "darwin", Arch: "arm64", Kind: "full", Component: name,
			URL: "https://download.example.test/" + name + ".zip", SHA256: hexSHA256(payload), Size: int64(len(payload)),
		})
	}
	payload := Payload{
		SchemaVersion: 1, Product: "mz-gui-desktop", Channel: "stable", Version: "1.5.0", BuildID: "build-150",
		PublishedAt: "2026-08-16T08:00:00Z", ExpiresAt: "2026-08-17T08:00:00Z", Artifacts: artifacts,
	}
	manifest, err := ParseAndVerify(signedManifest(t, "release-2026", privateKey, payload), VerifyPolicy{
		Product: "mz-gui-desktop", Channel: "stable",
		Keys: map[string]ed25519.PublicKey{"release-2026": publicKey},
		Now:  func() time.Time { return time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return processSetEnv{
		root: filepath.Join(root, "session"), installRoot: installRoot, receiptPath: filepath.Join(root, "receipt.json"),
		live: live, current: current, next: next, manifest: manifest,
	}
}

func (env processSetEnv) options(fetch func(context.Context, Artifact, string) error, replace func(string, string) error, validate func(context.Context, map[string]string) error) ProcessSetOptions {
	components := make([]LiveComponent, 0, len(env.live))
	sidecars := make([]ProcessArtifact, 0, len(env.live)-1)
	for _, name := range []string{ComponentDesktop, ComponentAgentd, ComponentHelper} {
		path, ok := env.live[name]
		if !ok {
			continue
		}
		components = append(components, LiveComponent{Component: name, LivePath: path, CurrentSHA256: hexSHA256(env.current[name])})
		if name != ComponentDesktop {
			sidecars = append(sidecars, ProcessArtifact{Name: name, Version: "1.4.0", BuildID: "build-140"})
		}
	}
	return ProcessSetOptions{
		Root: env.root, Manifest: env.manifest, OS: "darwin", Arch: "arm64", ReceiptPath: env.receiptPath,
		AllowedInstallRoots: []string{env.installRoot},
		Components:          components, Fetch: fetch, Replace: replace, Validate: validate,
		Current: ProcessSet{
			Desktop:  ProcessArtifact{Name: ComponentDesktop, Version: "1.4.0", BuildID: "build-140"},
			Sidecars: sidecars,
		},
	}
}

func (env processSetEnv) fetch() func(context.Context, Artifact, string) error {
	return func(_ context.Context, artifact Artifact, dest string) error {
		data, ok := env.next[artifact.Component]
		if !ok {
			return errors.New("missing component artifact")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o600)
	}
}

func (env processSetEnv) assertLive(t *testing.T, want map[string][]byte) {
	t.Helper()
	for name, path := range env.live {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want[name]) {
			t.Fatalf("%s live 内容错误: got=%q want=%q err=%v", name, got, want[name], err)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
