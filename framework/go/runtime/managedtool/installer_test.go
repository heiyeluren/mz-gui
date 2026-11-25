package managedtool

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/artifact"
)

func TestInstallerPublishesVerifiedPackageAndResolvesExecutable(t *testing.T) {
	archive := runtimeZIP(t, "bin/codex"+executableSuffix(), []byte("runtime-binary"))
	digest := sha256.Sum256(archive)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(archive)
	}))
	defer server.Close()

	root := t.TempDir()
	installer, err := New(Options{
		Root: filepath.Join(root, "managed-runtimes"), PackageRoot: filepath.Join(root, "packages"),
		AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")},
		Downloader:   artifact.Downloader{Client: server.Client()},
	})
	if err != nil {
		t.Fatalf("创建受管工具安装器失败: %v", err)
	}
	request := Package{
		ID: "codex", Version: "1.2.3", URL: server.URL + "/codex.zip", Format: FormatZIP,
		SHA256: hex.EncodeToString(digest[:]), Size: int64(len(archive)), Executable: "bin/codex" + executableSuffix(),
	}
	result, err := installer.Install(context.Background(), request)
	if err != nil {
		t.Fatalf("安装受管工具失败: %v", err)
	}
	if result.ID != "codex" || result.Version != "1.2.3" || result.Reused || result.Bytes != int64(len(archive)) {
		t.Fatalf("安装结果不正确: %#v", result)
	}
	if data, readErr := os.ReadFile(result.ExecutablePath); readErr != nil || string(data) != "runtime-binary" {
		t.Fatalf("受管可执行文件不正确: data=%q err=%v", data, readErr)
	}
	resolved, err := installer.Resolve("codex", "bin/codex"+executableSuffix())
	if err != nil || resolved != result.ExecutablePath {
		t.Fatalf("解析受管可执行文件失败: path=%q err=%v", resolved, err)
	}
	inspected, err := installer.Inspect("codex", "bin/codex"+executableSuffix())
	if err != nil || inspected.Version != "1.2.3" || inspected.SHA256 != request.SHA256 || inspected.ExecutablePath != resolved {
		t.Fatalf("受管工具快照错误: result=%#v err=%v", inspected, err)
	}

	reused, err := installer.Install(context.Background(), request)
	if err != nil || !reused.Reused || reused.ExecutablePath != result.ExecutablePath {
		t.Fatalf("重复安装没有安全复用: result=%#v err=%v", reused, err)
	}
}

func TestInstallerRejectsUnsafePackageBeforeDownload(t *testing.T) {
	root := t.TempDir()
	installer, err := New(Options{
		Root: filepath.Join(root, "managed-runtimes"), PackageRoot: filepath.Join(root, "packages"),
		AllowedHosts: []string{"packages.example.com"},
	})
	if err != nil {
		t.Fatalf("创建受管工具安装器失败: %v", err)
	}
	tests := []Package{
		{ID: "../codex", Version: "1.0.0", URL: "https://packages.example.com/codex.zip", Format: FormatZIP, SHA256: strings.Repeat("a", 64), Size: 1, Executable: "bin/codex"},
		{ID: "codex", Version: "1.0.0", URL: "https://packages.example.com/codex.zip", Format: FormatZIP, SHA256: strings.Repeat("a", 64), Size: 1, Executable: "../codex"},
		{ID: "codex", Version: "1.0.0", URL: "http://packages.example.com/codex.zip", Format: FormatZIP, SHA256: strings.Repeat("a", 64), Size: 1, Executable: "bin/codex"},
	}
	for _, request := range tests {
		if _, err := installer.Install(context.Background(), request); err == nil {
			t.Fatalf("不安全安装请求未被拒绝: %#v", request)
		}
	}
}

func TestInstallerRejectsInvalidOptionsAndCanceledInstall(t *testing.T) {
	root := t.TempDir()
	if _, err := New(Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("空配置错误=%v", err)
	}
	installer, err := New(Options{
		Root: filepath.Join(root, "managed"), PackageRoot: filepath.Join(root, "packages"), AllowedHosts: []string{"packages.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Resolve("codex", "bin/codex"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("未安装解析错误=%v", err)
	}
	if _, err := installer.Inspect("codex", "bin/codex"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("未安装快照错误=%v", err)
	}
	if _, err := installer.Resolve("../codex", "bin/codex"); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("非法解析错误=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := Package{ID: "codex", Version: "1.0.0", URL: "https://packages.example.com/codex.zip", Format: FormatZIP, SHA256: strings.Repeat("a", 64), Size: 1, Executable: "bin/codex"}
	if _, err := installer.Install(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消安装错误=%v", err)
	}
}

func TestInstallerDoesNotPublishPackageWithoutExecutable(t *testing.T) {
	archive := runtimeZIP(t, "README.txt", []byte("missing executable"))
	digest := sha256.Sum256(archive)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write(archive) }))
	defer server.Close()
	root := t.TempDir()
	installer, err := New(Options{
		Root: filepath.Join(root, "managed"), PackageRoot: filepath.Join(root, "packages"),
		AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")}, Downloader: artifact.Downloader{Client: server.Client()},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = installer.Install(context.Background(), Package{
		ID: "codex", Version: "1.0.0", URL: server.URL + "/codex.zip", Format: FormatZIP,
		SHA256: hex.EncodeToString(digest[:]), Size: int64(len(archive)), Executable: "bin/codex" + executableSuffix(),
	})
	if err == nil {
		t.Fatal("缺少可执行文件的包未被拒绝")
	}
	if _, err := installer.Resolve("codex", "bin/codex"+executableSuffix()); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("失败包被错误发布: %v", err)
	}
}

func TestInstallerRejectsUnsafeRootsAndCorruptedCache(t *testing.T) {
	root := t.TempDir()
	symlink := filepath.Join(root, "packages-link")
	if err := os.Symlink(root, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{Root: filepath.Join(root, "managed-a"), PackageRoot: symlink, AllowedHosts: []string{"packages.example.com"}}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("符号链接缓存根错误=%v", err)
	}
	packageRoot := filepath.Join(root, "packages")
	installer, err := New(Options{Root: filepath.Join(root, "managed-b"), PackageRoot: packageRoot, AllowedHosts: []string{"packages.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	cache := filepath.Join(packageRoot, "codex-1.0.0-"+digest[:16]+".zip")
	if err := os.WriteFile(cache, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = installer.Install(context.Background(), Package{
		ID: "codex", Version: "1.0.0", URL: "https://packages.example.com/codex.zip", Format: FormatZIP,
		SHA256: digest, Size: 42, Executable: "bin/codex",
	})
	if err == nil || !strings.Contains(err.Error(), "缓存包") {
		t.Fatalf("损坏缓存错误=%v", err)
	}
}

func runtimeZIP(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o700)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
