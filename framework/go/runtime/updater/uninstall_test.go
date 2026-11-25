package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallRemovesProcessArtifactsAndKeepsUserDataByDefault(t *testing.T) {
	env := newUninstallEnv(t)
	result, err := Uninstall(context.Background(), env.options(false, nil, nil))
	if err != nil {
		t.Fatalf("默认卸载应成功: %#v err=%v", result, err)
	}
	if !result.KeptUserData {
		t.Fatal("默认必须保留用户数据")
	}
	env.assertProcessGone(t)
	env.assertUserDataIntact(t)
	assertItemStatus(t, result.ProcessArtifacts, ComponentDesktop, ItemRemoved)
	assertItemStatus(t, result.ProcessArtifacts, ComponentAgentd, ItemRemoved)
	assertItemStatus(t, result.ProcessArtifacts, ComponentHelper, ItemRemoved)
	for _, kind := range userDataKinds() {
		assertItemStatus(t, result.UserData, kind, ItemKept)
	}
}

func TestUninstallDeletesUserDataWithItemizedResultsWhenRequested(t *testing.T) {
	env := newUninstallEnv(t)
	result, err := Uninstall(context.Background(), env.options(true, nil, nil))
	if err != nil || result.KeptUserData {
		t.Fatalf("明确删除数据应成功且不保留: %#v err=%v", result, err)
	}
	env.assertProcessGone(t)
	for _, item := range env.userData {
		if _, err := os.Lstat(item.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("应删除用户数据 %s: %v", item.Kind, err)
		}
	}
	for _, kind := range userDataKinds() {
		assertItemStatus(t, result.UserData, kind, ItemRemoved)
	}
}

func TestUninstallRestoresAllUserDataWhenDeletionFailsMidway(t *testing.T) {
	env := newUninstallEnv(t)
	result, err := Uninstall(context.Background(), env.options(true, nil, func(source, dest string) error {
		if strings.Contains(source, string(filepath.Separator)+UserDataCredentials+string(filepath.Separator)) ||
			strings.HasSuffix(source, string(filepath.Separator)+UserDataCredentials) {
			return errors.New("credentials locked")
		}
		return os.Rename(source, dest)
	}))
	if !errors.Is(err, ErrUninstallRolledBack) || !result.KeptUserData {
		t.Fatalf("中途失败必须回滚用户数据: %#v err=%v", result, err)
	}
	env.assertProcessGone(t)
	env.assertUserDataIntact(t)
	assertItemStatus(t, result.UserData, UserDataCredentials, ItemFailed)
	for _, kind := range []string{UserDataConfig, UserDataDatabase} {
		status := itemStatus(result.UserData, kind)
		if status != ItemRestored && status != ItemKept {
			t.Fatalf("%s 失败后不得保持半删除状态: %s", kind, status)
		}
	}
}

func TestUninstallRefusesUnmarkedInstallRoot(t *testing.T) {
	env := newUninstallEnv(t)
	if err := os.Remove(filepath.Join(env.drillRoot, DrillMarkerName)); err != nil {
		t.Fatal(err)
	}
	result, err := Uninstall(context.Background(), env.options(true, nil, nil))
	if !errors.Is(err, ErrUninstallRefused) {
		t.Fatalf("无演练标记必须拒绝: %#v err=%v", result, err)
	}
	for name, path := range env.live {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("拒绝后不得改动进程制品 %s: %v", name, err)
		}
	}
	env.assertUserDataIntact(t)
}

func TestUninstallRefusesSystemApplicationsRoot(t *testing.T) {
	result, err := Uninstall(context.Background(), UninstallOptions{
		DrillRoot:   "/Applications/mz-gui-desktop.app",
		InstallRoot: "/Applications/mz-gui-desktop.app",
		Components:  []ProcessTarget{{Component: ComponentDesktop, Path: "/Applications/mz-gui-desktop.app/Contents/MacOS/mz-gui-desktop"}},
	})
	if !errors.Is(err, ErrUninstallRefused) {
		t.Fatalf("真机 Applications 必须拒绝: %#v err=%v", result, err)
	}
}

func TestUninstallKeepsUserDataWhenProcessRemovalFails(t *testing.T) {
	env := newUninstallEnv(t)
	result, err := Uninstall(context.Background(), env.options(true, func(path string) error {
		if strings.Contains(filepath.Base(path), ComponentAgentd) {
			return errors.New("agentd busy")
		}
		return os.Remove(path)
	}, nil))
	if err == nil {
		t.Fatalf("进程制品移除失败应返回错误: %#v", result)
	}
	if !result.KeptUserData {
		t.Fatal("进程制品失败时不得删除用户数据")
	}
	env.assertUserDataIntact(t)
	if _, err := os.Lstat(env.live[ComponentAgentd]); err != nil {
		t.Fatalf("失败的 agentd 应仍在: %v", err)
	}
}

type uninstallEnv struct {
	drillRoot   string
	installRoot string
	live        map[string]string
	payloads    map[string][]byte
	userData    []UserDataItem
	contents    map[string][]byte
}

func newUninstallEnv(t *testing.T) uninstallEnv {
	t.Helper()
	drillRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(drillRoot, DrillMarkerName), []byte("mzgui.uninstall.drill.v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installRoot := filepath.Join(drillRoot, "app")
	dataRoot := filepath.Join(drillRoot, "data")
	if err := os.MkdirAll(installRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	payloads := map[string][]byte{
		ComponentDesktop: []byte("LIVE-desktop"),
		ComponentAgentd:  []byte("LIVE-agentd"),
		ComponentHelper:  []byte("LIVE-helper"),
	}
	live := map[string]string{}
	for name, payload := range payloads {
		path := filepath.Join(installRoot, name+".bin")
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		live[name] = path
	}
	contents := map[string][]byte{}
	items := make([]UserDataItem, 0, len(userDataKinds()))
	for _, kind := range userDataKinds() {
		dir := filepath.Join(dataRoot, kind)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "payload")
		body := []byte("USER-" + kind)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		contents[kind] = body
		items = append(items, UserDataItem{Kind: kind, Path: path})
	}
	return uninstallEnv{
		drillRoot: drillRoot, installRoot: installRoot, live: live, payloads: payloads,
		userData: items, contents: contents,
	}
}

func (env uninstallEnv) options(deleteData bool, remove func(string) error, quarantine func(string, string) error) UninstallOptions {
	components := make([]ProcessTarget, 0, len(env.live))
	for _, name := range []string{ComponentDesktop, ComponentAgentd, ComponentHelper} {
		components = append(components, ProcessTarget{Component: name, Path: env.live[name]})
	}
	return UninstallOptions{
		DrillRoot: env.drillRoot, InstallRoot: env.installRoot, Components: components,
		UserData: env.userData, DeleteUserData: deleteData, Remove: remove, Quarantine: quarantine,
	}
}

func (env uninstallEnv) assertProcessGone(t *testing.T) {
	t.Helper()
	for name, path := range env.live {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("进程制品 %s 应被移除: %v", name, err)
		}
	}
}

func (env uninstallEnv) assertUserDataIntact(t *testing.T) {
	t.Helper()
	for _, item := range env.userData {
		got, err := os.ReadFile(item.Path)
		if err != nil || string(got) != string(env.contents[item.Kind]) {
			t.Fatalf("用户数据 %s 必须完整保留: got=%q err=%v", item.Kind, got, err)
		}
	}
}

func userDataKinds() []string {
	return []string{
		UserDataConfig, UserDataDatabase, UserDataCache, UserDataLogs,
		UserDataCredentials, UserDataStartup, UserDataProtocol,
	}
}

func assertItemStatus(t *testing.T, items []UninstallItemResult, kind, want string) {
	t.Helper()
	if got := itemStatus(items, kind); got != want {
		t.Fatalf("%s 状态应为 %s，实际 %s", kind, want, got)
	}
}

func itemStatus(items []UninstallItemResult, kind string) string {
	for _, item := range items {
		if item.Kind == kind {
			return item.Status
		}
	}
	return ""
}
