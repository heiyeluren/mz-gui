package startup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveSiblingExecutablePrefersBundleOverPATH(t *testing.T) {
	root := t.TempDir()
	bundleMacOS := filepath.Join(root, "Mozi APP.app", "Contents", "MacOS")
	pathDir := filepath.Join(root, "path-bin")
	if err := os.MkdirAll(bundleMacOS, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pathDir, 0o700); err != nil {
		t.Fatal(err)
	}
	bundleDesktop := filepath.Join(bundleMacOS, "sample-desktop")
	bundleSidecar := filepath.Join(bundleMacOS, "sample-worker")
	pathDesktop := filepath.Join(pathDir, "sample-desktop")
	pathSidecar := filepath.Join(pathDir, "sample-worker")
	for _, item := range []struct{ path, body string }{
		{bundleDesktop, "bundle-desktop"},
		{bundleSidecar, "bundle-sidecar"},
		{pathSidecar, "path-sidecar"},
	} {
		if err := os.WriteFile(item.path, []byte(item.body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	createFileLinkLike(t, bundleDesktop, pathDesktop)

	resolved, err := ResolveSiblingExecutable(pathDesktop, "sample-worker", "darwin")
	if err != nil || !sameResolvedPath(t, resolved, bundleSidecar) || sameResolvedPath(t, resolved, pathSidecar) {
		t.Fatalf("bundle 内 sidecar 必须优先于 PATH: resolved=%q err=%v", resolved, err)
	}
}

func TestResolveSiblingExecutableUsesSameVersionSiblingInsteadOfPATH(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "install")
	pathDir := filepath.Join(root, "path-bin")
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pathDir, 0o700); err != nil {
		t.Fatal(err)
	}
	installDesktop := filepath.Join(installDir, "sample-desktop")
	installSidecar := filepath.Join(installDir, "sample-worker")
	pathDesktop := filepath.Join(pathDir, "sample-desktop")
	pathSidecar := filepath.Join(pathDir, "sample-worker")
	for _, item := range []struct{ path, body string }{
		{installDesktop, "install-desktop"},
		{installSidecar, "install-sidecar"},
		{pathSidecar, "path-sidecar"},
	} {
		if err := os.WriteFile(item.path, []byte(item.body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	createFileLinkLike(t, installDesktop, pathDesktop)

	resolved, err := ResolveSiblingExecutable(pathDesktop, "sample-worker", "darwin")
	if err != nil || !sameResolvedPath(t, resolved, installSidecar) || sameResolvedPath(t, resolved, pathSidecar) {
		t.Fatalf("同版本 sidecar 必须优先于 PATH: resolved=%q err=%v", resolved, err)
	}
	direct, err := ResolveSiblingExecutable(installDesktop, "sample-worker", "darwin")
	if err != nil || direct != installSidecar {
		t.Fatalf("安装目录 sidecar 必须保持同版本: resolved=%q err=%v", direct, err)
	}
}

func sameResolvedPath(t *testing.T, got, want string) bool {
	t.Helper()
	if got == want {
		return true
	}
	gotInfo, gotErr := os.Stat(got)
	wantInfo, wantErr := os.Stat(want)
	if gotErr != nil || wantErr != nil {
		return false
	}
	return os.SameFile(gotInfo, wantInfo)
}

func TestResolveSiblingExecutableUsesInjectedSafeName(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "sample-desktop")
	sidecar := filepath.Join(root, "sample-worker")
	for _, path := range []string{desktop, sidecar} {
		if err := os.WriteFile(path, []byte("binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := ResolveSiblingExecutable(desktop, "sample-worker", "darwin")
	if err != nil || resolved != sidecar {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
	for _, name := range []string{"", "../worker", "worker/name", "worker name"} {
		if _, err := ResolveSiblingExecutable(desktop, name, "darwin"); !errors.Is(err, ErrInvalidSidecarName) {
			t.Fatalf("名称 %q 错误=%v", name, err)
		}
	}
}

func TestResolveSiblingExecutableRejectsSymbolicLink(t *testing.T) {
	root := t.TempDir()
	desktopName := "sample-desktop"
	sidecarName := "sample-worker"
	targetName := "real-worker"
	if runtime.GOOS == "windows" {
		desktopName += ".exe"
		sidecarName += ".exe"
		targetName += ".exe"
	}
	desktop := filepath.Join(root, desktopName)
	target := filepath.Join(root, targetName)
	link := filepath.Join(root, sidecarName)
	if err := os.WriteFile(desktop, []byte("desktop"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("worker"), 0o700); err != nil {
		t.Fatal(err)
	}
	createFileLinkLike(t, target, link)
	if _, err := ResolveSiblingExecutable(desktop, "sample-worker", runtime.GOOS); !errors.Is(err, ErrUnsafeSidecar) {
		t.Fatalf("符号链接错误=%v", err)
	}
}

func TestResolveSiblingExecutableRejectsMissingUnsupportedAndNonExecutableSidecar(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "sample-desktop")
	if err := os.WriteFile(desktop, []byte("desktop"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveSiblingExecutable(desktop, "missing-worker", "darwin"); err == nil {
		t.Fatal("缺失 sidecar 必须拒绝")
	}
	if _, err := ResolveSiblingExecutable(desktop, "sample-worker", "plan9"); !errors.Is(err, ErrUnsupportedOS) {
		t.Fatalf("平台错误=%v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	worker := filepath.Join(root, "sample-worker")
	if err := os.WriteFile(worker, []byte("worker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveSiblingExecutable(desktop, "sample-worker", runtime.GOOS); err == nil {
		t.Fatal("不可执行 sidecar 必须拒绝")
	}
}

func TestResolveSiblingExecutableWindowsUsesExeSuffix(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "sample-desktop.exe")
	sidecar := filepath.Join(root, "sample-worker.exe")
	plain := filepath.Join(root, "sample-worker")
	for _, path := range []string{desktop, sidecar, plain} {
		if err := os.WriteFile(path, []byte("binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := ResolveSiblingExecutable(desktop, "sample-worker", "windows")
	if err != nil || resolved != sidecar {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveSiblingExecutable(desktop, "sample-worker", "windows"); err == nil {
		t.Fatal("缺少 .exe 的 Windows sidecar 必须拒绝")
	}
}

func TestLauncherResolvesAndStartsInjectedSidecar(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "sample-desktop")
	sidecar := filepath.Join(root, "sample-worker")
	for _, path := range []string{desktop, sidecar} {
		if err := os.WriteFile(path, []byte("binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var started string
	launcher := NewLauncher(LauncherOptions{
		SidecarName: "sample-worker", GOOS: "darwin",
		DesktopExecutable: func() (string, error) { return desktop, nil },
		StartDetached:     func(_ context.Context, path string) error { started = path; return nil },
	})
	if err := launcher.Start(context.Background()); err != nil || started != sidecar {
		t.Fatalf("started=%q err=%v", started, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := launcher.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误=%v", err)
	}
}

func TestLauncherPropagatesExecutableAndStartErrors(t *testing.T) {
	wantExecutable := errors.New("executable failed")
	launcher := NewLauncher(LauncherOptions{
		SidecarName: "sample-worker", GOOS: "darwin",
		DesktopExecutable: func() (string, error) { return "", wantExecutable },
		StartDetached:     func(context.Context, string) error { return nil },
	})
	if err := launcher.Start(context.Background()); !errors.Is(err, wantExecutable) {
		t.Fatalf("executable err=%v", err)
	}

	root := t.TempDir()
	desktop := filepath.Join(root, "sample-desktop")
	worker := filepath.Join(root, "sample-worker")
	for _, path := range []string{desktop, worker} {
		if err := os.WriteFile(path, []byte("binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	wantStart := errors.New("start failed")
	launcher = NewLauncher(LauncherOptions{
		SidecarName: "sample-worker", GOOS: "darwin",
		DesktopExecutable: func() (string, error) { return desktop, nil },
		StartDetached:     func(context.Context, string) error { return wantStart },
	})
	if err := launcher.Start(context.Background()); !errors.Is(err, wantStart) {
		t.Fatalf("start err=%v", err)
	}
}
