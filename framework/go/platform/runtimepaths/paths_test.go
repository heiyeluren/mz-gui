package runtimepaths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveDarwinPaths(t *testing.T) {
	paths, err := Resolve("darwin", testLayout(), Inputs{
		HomeDir:      "/Users/tester",
		UserCacheDir: "/Users/tester/Library/Caches",
		TempDir:      "/private/var/folders/example/T",
		UID:          "501",
	})
	if err != nil {
		t.Fatalf("解析 macOS 路径: %v", err)
	}

	assertPath(t, paths.Root, "/Users/tester/Library/Application Support/Acme Desk")
	assertPath(t, paths.ConfigFile, "/Users/tester/Library/Application Support/Acme Desk/config/acme.toml")
	assertPath(t, paths.DatabaseFile, "/Users/tester/Library/Application Support/Acme Desk/data/acme.db")
	assertPath(t, paths.LogsDir, "/Users/tester/Library/Logs/Acme Desk")
	assertPath(t, paths.CacheDir, "/Users/tester/Library/Caches/Acme Desk")
	assertPath(t, paths.RunDir, "/private/var/folders/example/T/acme-gui-501")
	for _, value := range []string{paths.Root, paths.ConfigFile, paths.DatabaseFile, paths.LogsDir, paths.CacheDir, paths.RunDir} {
		if strings.Contains(value, `\`) {
			t.Fatalf("darwin 路径必须使用 /，不能跟 host GOOS 变成反斜杠: %q", value)
		}
	}
}

func TestResolveWindowsPaths(t *testing.T) {
	paths, err := Resolve("windows", testLayout(), Inputs{
		LocalAppData: "C:\\Users\\tester\\AppData\\Local",
	})
	if err != nil {
		t.Fatalf("解析 Windows 路径: %v", err)
	}

	assertPath(t, paths.Root, "C:\\Users\\tester\\AppData\\Local\\Acme Desk")
	assertPath(t, paths.ConfigFile, "C:\\Users\\tester\\AppData\\Local\\Acme Desk\\config\\acme.toml")
	assertPath(t, paths.DatabaseFile, "C:\\Users\\tester\\AppData\\Local\\Acme Desk\\data\\acme.db")
	assertPath(t, paths.LogsDir, "C:\\Users\\tester\\AppData\\Local\\Acme Desk\\logs")
	assertPath(t, paths.RunDir, "C:\\Users\\tester\\AppData\\Local\\Acme Desk\\run")
}

func TestResolveRejectsUnsupportedPlatform(t *testing.T) {
	if _, err := Resolve("plan9", testLayout(), Inputs{}); err == nil {
		t.Fatal("不支持的平台必须返回错误")
	}
}

func TestDevelopmentRootRequiresAbsolutePath(t *testing.T) {
	if _, err := DevelopmentRoot(testLayout(), "relative/path"); err == nil {
		t.Fatal("开发运行根目录必须拒绝相对路径")
	}
	paths, err := DevelopmentRoot(testLayout(), filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(paths.DatabaseFile) != "acme.db" {
		t.Fatalf("开发数据库路径错误: %s", paths.DatabaseFile)
	}
	if filepath.Base(paths.ConfigFile) != "acme.toml" {
		t.Fatalf("开发配置路径错误: %s", paths.ConfigFile)
	}
}

func TestEnsureCreatesPrivateDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime")
	paths := build(testLayout(), filepath.Join, root, filepath.Join(root, "logs"), filepath.Join(root, "cache"), filepath.Join(root, "run"))

	if err := Ensure(paths); err != nil {
		t.Fatalf("创建运行目录: %v", err)
	}
	for _, directory := range paths.Directories() {
		assertPrivateDirectory(t, directory)
	}
}

func TestEnsurePrivateDirectoryCreatesNestedPrivateDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "workspaces", "default")
	if err := EnsurePrivateDirectory(directory); err != nil {
		t.Fatalf("创建私有目录失败: %v", err)
	}
	assertPrivateDirectory(t, directory)
}

func TestVerifyPrivateRegularFileAcceptsOwnedFileAndRejectsLinks(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private")
	if err := EnsurePrivateDirectory(directory); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "state.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPrivateRegularFile(path); err != nil {
		t.Fatalf("当前用户私有文件应通过校验: %v", err)
	}
	linked := filepath.Join(directory, "linked.json")
	createLinkLike(t, path, linked)
	if _, err := VerifyPrivateRegularFile(linked); err == nil {
		t.Fatal("链接文件必须拒绝")
	}
}

func TestVerifyPrivateRegularFileRejectsBroadUnixPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 使用 ACL 校验")
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPrivateRegularFile(path); err == nil {
		t.Fatal("组或其他用户可读的文件必须拒绝")
	}
}

func TestEnsureRejectsSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedRoot := filepath.Join(base, "linked")
	createLinkLike(t, realRoot, linkedRoot)

	paths := build(testLayout(), filepath.Join, linkedRoot, filepath.Join(linkedRoot, "logs"), filepath.Join(linkedRoot, "cache"), filepath.Join(linkedRoot, "run"))
	if err := Ensure(paths); err == nil {
		t.Fatal("运行时根目录为符号链接时必须拒绝")
	}
}

func TestResolveRejectsMissingTrustedSystemDirectories(t *testing.T) {
	if _, err := Resolve("darwin", testLayout(), Inputs{HomeDir: "/Users/test"}); err == nil {
		t.Fatal("macOS 系统目录不完整时必须拒绝")
	}
	if _, err := Resolve("windows", testLayout(), Inputs{}); err == nil {
		t.Fatal("Windows LocalAppData 缺失时必须拒绝")
	}
}

func TestEnsureRejectsEmptyAndOccupiedDirectory(t *testing.T) {
	if err := Ensure(Paths{}); err == nil {
		t.Fatal("空运行目录必须拒绝")
	}

	root := filepath.Join(t.TempDir(), "runtime")
	if err := os.WriteFile(root, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := build(testLayout(), filepath.Join, root, filepath.Join(root, "logs"), filepath.Join(root, "cache"), filepath.Join(root, "run"))
	if err := Ensure(paths); err == nil {
		t.Fatal("普通文件占用运行目录时必须拒绝")
	}
}

func TestDevelopmentOverrideIsIsolatedFromProduction(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime")
	layout := testLayout()
	t.Setenv(layout.RootOverrideEnv, root)
	t.Setenv(layout.EnvironmentEnv, "production")
	if _, overridden, err := developmentOverride(layout); !overridden || err == nil {
		t.Fatal("生产环境必须拒绝开发目录覆盖")
	}

	t.Setenv(layout.EnvironmentEnv, "test")
	paths, overridden, err := developmentOverride(layout)
	if err != nil || !overridden || paths.Root != root {
		t.Fatalf("测试环境目录覆盖失败: paths=%#v overridden=%v err=%v", paths, overridden, err)
	}
	current, err := Current(layout)
	if err != nil || current.Root != root {
		t.Fatalf("Current 未使用测试隔离目录: paths=%#v err=%v", current, err)
	}
}

func TestDevelopmentOverrideIsOptional(t *testing.T) {
	layout := testLayout()
	t.Setenv(layout.RootOverrideEnv, "")
	if _, overridden, err := developmentOverride(layout); overridden || err != nil {
		t.Fatalf("未配置覆盖目录时不应生效: overridden=%v err=%v", overridden, err)
	}
}

func TestLayoutRejectsUnsafeOrIncompleteProductValues(t *testing.T) {
	tests := []Layout{
		{},
		{ProductDirectory: "../escape", ConfigFile: "acme.toml", DatabaseFile: "acme.db", RuntimePrefix: "acme", RootOverrideEnv: "ACME_ROOT", EnvironmentEnv: "ACME_ENV"},
		{ProductDirectory: "Acme", ConfigFile: "nested/acme.toml", DatabaseFile: "acme.db", RuntimePrefix: "acme", RootOverrideEnv: "ACME_ROOT", EnvironmentEnv: "ACME_ENV"},
		{ProductDirectory: "Acme", ConfigFile: "acme.toml", DatabaseFile: "nested/acme.db", RuntimePrefix: "acme", RootOverrideEnv: "ACME_ROOT", EnvironmentEnv: "ACME_ENV"},
		{ProductDirectory: "Acme", ConfigFile: "acme.toml", DatabaseFile: "acme.db", RuntimePrefix: "../acme", RootOverrideEnv: "ACME_ROOT", EnvironmentEnv: "ACME_ENV"},
		{ProductDirectory: "Acme:Desk", ConfigFile: "acme.toml", DatabaseFile: "acme.db", RuntimePrefix: "acme", RootOverrideEnv: "ACME_ROOT", EnvironmentEnv: "ACME_ENV"},
		{ProductDirectory: "Acme", ConfigFile: "CON", DatabaseFile: "acme.db", RuntimePrefix: "acme", RootOverrideEnv: "ACME_ROOT", EnvironmentEnv: "ACME_ENV"},
		{ProductDirectory: "Acme", ConfigFile: "acme.toml", DatabaseFile: "CON", RuntimePrefix: "acme", RootOverrideEnv: "ACME_ROOT", EnvironmentEnv: "ACME_ENV"},
		{ProductDirectory: "Acme", ConfigFile: "acme.toml", DatabaseFile: "acme.db", RuntimePrefix: "bad*prefix", RootOverrideEnv: "ACME_ROOT", EnvironmentEnv: "ACME_ENV"},
	}
	for _, layout := range tests {
		if _, err := DevelopmentRoot(layout, filepath.Join(t.TempDir(), "runtime")); err == nil {
			t.Fatalf("非法产品布局必须拒绝: %#v", layout)
		}
	}
}

func testLayout() Layout {
	return Layout{
		ProductDirectory: "Acme Desk",
		ConfigFile:       "acme.toml",
		DatabaseFile:     "acme.db",
		RuntimePrefix:    "acme-gui",
		RootOverrideEnv:  "ACME_RUNTIME_ROOT",
		EnvironmentEnv:   "ACME_ENV",
	}
}

func assertPath(t *testing.T, actual, expected string) {
	t.Helper()
	if actual != expected {
		t.Fatalf("路径不一致: got %q, want %q", actual, expected)
	}
}

func assertPrivateDirectory(t *testing.T, directory string) {
	t.Helper()
	info, err := os.Lstat(directory)
	if err != nil {
		t.Fatalf("读取目录 %s: %v", directory, err)
	}
	if isLinkLike(info) {
		t.Fatalf("运行目录不能是符号链接: %s", directory)
	}
	if !info.IsDir() {
		t.Fatalf("不是目录: %s", directory)
	}
	if runtime.GOOS == "windows" {
		if ownerOnly, queryable := windowsOwnerOnly(directory); queryable && !ownerOnly {
			t.Fatalf("Windows 目录 ACL 不是 owner-only: %s", directory)
		}
		return
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("目录权限不安全: %s mode=%o", directory, info.Mode().Perm())
	}
}

func TestInstanceDirectoryDerivesIsolatedRoots(t *testing.T) {
	layout := testLayout()
	layout.InstanceDirectory = "0123456789ab"
	darwin, err := Resolve("darwin", layout, Inputs{HomeDir: "/Users/tester", UserCacheDir: "/Users/tester/Library/Caches", TempDir: "/tmp", UID: "501"})
	if err != nil {
		t.Fatal(err)
	}
	assertPath(t, darwin.Root, "/Users/tester/Library/Application Support/Acme Desk/instances/0123456789ab")
	assertPath(t, darwin.LogsDir, "/Users/tester/Library/Logs/Acme Desk/instances/0123456789ab")
	assertPath(t, darwin.CacheDir, "/Users/tester/Library/Caches/Acme Desk/instances/0123456789ab")
	assertPath(t, darwin.RunDir, "/tmp/acme-gui-501-0123456789ab")
	assertPath(t, darwin.DatabaseFile, "/Users/tester/Library/Application Support/Acme Desk/instances/0123456789ab/data/acme.db")

	windows, err := Resolve("windows", layout, Inputs{LocalAppData: `C:\Users\tester\AppData\Local`})
	if err != nil {
		t.Fatal(err)
	}
	assertPath(t, windows.Root, `C:\Users\tester\AppData\Local\Acme Desk\instances\0123456789ab`)
	assertPath(t, windows.RunDir, `C:\Users\tester\AppData\Local\Acme Desk\instances\0123456789ab\run`)

	development, err := DevelopmentRoot(layout, filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(development.Root)) != "instances" || filepath.Base(development.Root) != "0123456789ab" {
		t.Fatalf("开发根目录应带实例子目录: %s", development.Root)
	}
}

func TestEmptyInstanceDirectoryKeepsLegacyPaths(t *testing.T) {
	layout := testLayout()
	legacy, err := Resolve("darwin", layout, Inputs{HomeDir: "/Users/tester", UserCacheDir: "/Users/tester/Library/Caches", TempDir: "/tmp", UID: "501"})
	if err != nil {
		t.Fatal(err)
	}
	assertPath(t, legacy.Root, "/Users/tester/Library/Application Support/Acme Desk")
	assertPath(t, legacy.RunDir, "/tmp/acme-gui-501")
}

func TestInstanceDirectoryRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{"../x", "ABCDEF12", "short", "0123456789abcdef0123456789abcdef0", "01234567/89"} {
		layout := testLayout()
		layout.InstanceDirectory = value
		if _, err := DevelopmentRoot(layout, filepath.Join(t.TempDir(), "runtime")); err == nil {
			t.Fatalf("非法实例目录名必须拒绝: %q", value)
		}
	}
}

func TestResolveLinuxSeparatesCacheAndInstances(t *testing.T) {
	input := Inputs{HomeDir: "/home/tester", UserCacheDir: "/home/tester/.cache", TempDir: "/tmp", UID: "1000"}
	paths, err := Resolve("linux", testLayout(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertPath(t, paths.Root, "/home/tester/.local/share/Acme Desk")
	assertPath(t, paths.CacheDir, "/home/tester/.cache/Acme Desk")
	layout := testLayout()
	layout.InstanceDirectory = "12345678"
	second, err := Resolve("linux", layout, input)
	if err != nil {
		t.Fatal(err)
	}
	if second.Root == paths.Root || second.CacheDir == paths.CacheDir || second.RunDir == paths.RunDir {
		t.Fatal("Linux 实例目录未隔离")
	}
}
