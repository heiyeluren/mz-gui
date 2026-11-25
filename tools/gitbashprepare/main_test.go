package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/platform/toolruntime"
)

func TestPrepareDownloadsVerifiesAndBuildsTrustedRuntime(t *testing.T) {
	archive := []byte("portable git")
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = writer.Write(archive)
	}))
	defer server.Close()
	digest := sha256.Sum256(archive)
	manifestPath := writeRelease(t, releaseManifest{SchemaVersion: 1, RuntimeID: runtimeID, Version: "2.55.0.4", Artifacts: []releaseArtifact{
		{Arch: "amd64", VendorArch: "64-bit", URL: "https://github.com/git-for-windows/git/releases/download/v2.55.0.windows.4/PortableGit-2.55.0.4-64-bit.7z.exe", Size: int64(len(archive)), SHA256: hex.EncodeToString(digest[:])},
		{Arch: "arm64", VendorArch: "arm64", URL: "https://github.com/git-for-windows/git/releases/download/v2.55.0.windows.4/PortableGit-2.55.0.4-arm64.7z.exe", Size: int64(len(archive)), SHA256: hex.EncodeToString(digest[:])},
	}})
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Transport = rewriteTransport{base: client.Transport, target: target}
	options := prepareOptions{ManifestPath: manifestPath, Arch: "amd64", CacheRoot: t.TempDir(), HTTPClient: client, Extract: func(_ context.Context, _ string, destination string) error {
		return writePortableFixture(t, destination, "mingw64", 0x8664)
	}}
	path, manifestDigest, err := prepare(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d", requests.Load())
	}
	if _, err := toolruntime.ValidateDirectory(path, runtimeID, "2.55.0.4", "amd64", manifestDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "mingw64", "libexec", "git-core", "dlls-copied")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReleaseRejectsUntrustedManifest(t *testing.T) {
	base := releaseManifest{SchemaVersion: 1, RuntimeID: runtimeID, Version: "2.55.0.4", Artifacts: []releaseArtifact{
		{Arch: "amd64", VendorArch: "64-bit", URL: "https://github.com/git-for-windows/git/releases/download/v2.55.0.windows.4/PortableGit-2.55.0.4-64-bit.7z.exe", Size: 10, SHA256: strings.Repeat("a", 64)},
		{Arch: "arm64", VendorArch: "arm64", URL: "https://github.com/git-for-windows/git/releases/download/v2.55.0.windows.4/PortableGit-2.55.0.4-arm64.7z.exe", Size: 10, SHA256: strings.Repeat("b", 64)},
	}}
	for _, test := range []struct {
		name   string
		mutate func(*releaseManifest)
	}{
		{name: "schema", mutate: func(value *releaseManifest) { value.SchemaVersion = 2 }},
		{name: "runtime", mutate: func(value *releaseManifest) { value.RuntimeID = "other" }},
		{name: "version", mutate: func(value *releaseManifest) { value.Version = "../2.55" }},
		{name: "architectures", mutate: func(value *releaseManifest) { value.Artifacts = value.Artifacts[:1] }},
		{name: "host", mutate: func(value *releaseManifest) {
			value.Artifacts[0].URL = "https://example.test/PortableGit-2.55.0.4-64-bit.7z.exe"
		}},
		{name: "filename", mutate: func(value *releaseManifest) { value.Artifacts[0].URL += ".bad" }},
		{name: "digest", mutate: func(value *releaseManifest) { value.Artifacts[0].SHA256 = "bad" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := base
			value.Artifacts = append([]releaseArtifact(nil), base.Artifacts...)
			test.mutate(&value)
			if _, _, err := loadRelease(writeRelease(t, value), "amd64"); err == nil {
				t.Fatal("不可信 Git Bash 发布清单必须拒绝")
			}
		})
	}
}

func TestBuildRuntimeManifestRejectsUnexpectedProtectedFile(t *testing.T) {
	root := t.TempDir()
	if err := writePortableFixture(t, root, "mingw64", 0x8664); err != nil {
		t.Fatal(err)
	}
	if err := prepareGitDLLs(root, "mingw64"); err != nil {
		t.Fatal(err)
	}
	data, digest, err := buildRuntimeManifest(root, "2.55.0.4", "amd64", "amd64", []string{"usr/bin", "mingw64/bin", "mingw64/libexec/git-core", "cmd"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, toolruntime.ManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "usr", "bin", "injected.exe"), []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := toolruntime.ValidateDirectory(root, runtimeID, "2.55.0.4", "amd64", digest); err == nil {
		t.Fatal("保护目录新增文件必须拒绝")
	}
}

type rewriteTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (transport rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = transport.target.Scheme
	clone.URL.Host = transport.target.Host
	return transport.base.RoundTrip(clone)
}

func writeRelease(t *testing.T, manifest releaseManifest) string {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writePortableFixture(t *testing.T, root, vendorRoot string, machine uint16) error {
	t.Helper()
	for _, directory := range []string{"usr/bin", "cmd", vendorRoot + "/bin", vendorRoot + "/libexec/git-core"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o700); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(root, "post-install.bat"), []byte("post install"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "usr", "bin", "msys-2.0.dll"), []byte("msys"), 0o600); err != nil {
		return err
	}
	for _, relative := range []string{"usr/bin/bash.exe", vendorRoot + "/bin/git.exe"} {
		data := make([]byte, 96)
		data[0], data[1] = 'M', 'Z'
		binary.LittleEndian.PutUint32(data[0x3c:], 0x40)
		copy(data[0x40:], []byte{'P', 'E', 0, 0})
		binary.LittleEndian.PutUint16(data[0x44:], machine)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(relative)), data, 0o700); err != nil {
			return err
		}
	}
	for relative, value := range map[string]string{
		"usr/bin/tool.exe": "tool", "cmd/git.exe": "cmd", vendorRoot + "/bin/runtime.dll": "dll", vendorRoot + "/libexec/git-core/git-add.exe": "git-add",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(relative)), []byte(value), 0o600); err != nil {
			return err
		}
	}
	return nil
}
