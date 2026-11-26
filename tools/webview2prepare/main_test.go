package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
)

func TestPrepareDownloadsVerifiesAndReusesPinnedArtifact(t *testing.T) {
	archive := []byte("pinned webview2 cab")
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = writer.Write(archive)
	}))
	defer server.Close()

	sum := sha256.Sum256(archive)
	manifestPath := writeTestManifest(t, releaseManifest{
		SchemaVersion: 1,
		Version:       "151.0.4129.86",
		Artifacts: []releaseArtifact{
			{
				Arch: "amd64", VendorArch: "x64", URL: server.URL + "/runtime.cab",
				Size: int64(len(archive)), SHA256: hex.EncodeToString(sum[:]), ArchiveRoot: "Microsoft.WebView2.FixedVersionRuntime.151.0.4129.86.x64",
			},
			{
				Arch: "arm64", VendorArch: "arm64", URL: server.URL + "/runtime-arm64.cab",
				Size: int64(len(archive)), SHA256: hex.EncodeToString(sum[:]), ArchiveRoot: "Microsoft.WebView2.FixedVersionRuntime.151.0.4129.86.arm64",
			},
		},
	})
	cacheRoot := t.TempDir()
	extractor := func(_ context.Context, _ string, destination string) error {
		return writeRuntimeTree(destination, "Microsoft.WebView2.FixedVersionRuntime.151.0.4129.86.x64", "151.0.4129.86", 0x8664, "x64")
	}
	options := prepareOptions{
		ManifestPath: manifestPath, Arch: "amd64", CacheRoot: cacheRoot,
		HTTPClient: server.Client(), Extract: extractor,
	}
	first, err := prepare(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepare(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || requests.Load() != 1 {
		t.Fatalf("first=%q second=%q requests=%d", first, second, requests.Load())
	}
	if _, err := os.Stat(filepath.Join(first, "msedgewebview2.exe")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReleaseRejectsUntrustedManifest(t *testing.T) {
	base := releaseManifest{
		SchemaVersion: 1,
		Version:       "151.0.4129.86",
		Artifacts: []releaseArtifact{
			{
				Arch: "amd64", VendorArch: "x64", URL: "https://example.test/runtime.cab",
				Size: 10, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ArchiveRoot: "Microsoft.WebView2.FixedVersionRuntime.151.0.4129.86.x64",
			},
			{
				Arch: "arm64", VendorArch: "arm64", URL: "https://example.test/runtime-arm64.cab",
				Size: 10, SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				ArchiveRoot: "Microsoft.WebView2.FixedVersionRuntime.151.0.4129.86.arm64",
			},
		},
	}
	for _, test := range []struct {
		name   string
		mutate func(*releaseManifest)
	}{
		{name: "schema", mutate: func(value *releaseManifest) { value.SchemaVersion = 2 }},
		{name: "version", mutate: func(value *releaseManifest) { value.Version = "../151" }},
		{name: "incomplete architectures", mutate: func(value *releaseManifest) { value.Artifacts = value.Artifacts[:1] }},
		{name: "http", mutate: func(value *releaseManifest) { value.Artifacts[0].URL = "http://example.test/runtime.cab" }},
		{name: "hash", mutate: func(value *releaseManifest) { value.Artifacts[0].SHA256 = "bad" }},
		{name: "root", mutate: func(value *releaseManifest) { value.Artifacts[0].ArchiveRoot = "../runtime" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := base
			value.Artifacts = append([]releaseArtifact(nil), base.Artifacts...)
			test.mutate(&value)
			if _, _, err := loadRelease(writeTestManifest(t, value), "amd64"); err == nil {
				t.Fatal("不可信发布清单必须拒绝")
			}
		})
	}
}

func TestVerifyFileRejectsSymbolicLinkCache(t *testing.T) {
	testkit.RequireSymlink(t)
	root := t.TempDir()
	target := filepath.Join(root, "target.cab")
	data := []byte("trusted bytes")
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "runtime.cab")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if err := verifyFile(link, int64(len(data)), hex.EncodeToString(sum[:])); err == nil {
		t.Fatal("符号链接缓存必须拒绝")
	}
}

func TestDownloadRejectsHTTPSRedirectToHTTP(t *testing.T) {
	data := []byte("runtime")
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(data)
	}))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL+"/runtime.cab", http.StatusFound)
	}))
	defer redirect.Close()
	sum := sha256.Sum256(data)
	err := download(context.Background(), redirect.Client(), releaseArtifact{
		URL: redirect.URL + "/runtime.cab", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]),
	}, filepath.Join(t.TempDir(), "runtime.cab"))
	if err == nil {
		t.Fatal("HTTPS 降级重定向必须拒绝")
	}
}

func writeTestManifest(t *testing.T, manifest releaseManifest) string {
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

func writeRuntimeTree(destination, archiveRoot, version string, machine uint16, dllArch string) error {
	runtimeDirectory := filepath.Join(destination, archiveRoot)
	if err := os.MkdirAll(filepath.Join(runtimeDirectory, "EBWebView", dllArch), 0o700); err != nil {
		return err
	}
	for _, name := range []string{"msedge.dll", "icudtl.dat", "resources.pak", version + ".manifest"} {
		if err := os.WriteFile(filepath.Join(runtimeDirectory, name), []byte(name), 0o600); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(runtimeDirectory, "EBWebView", dllArch, "EmbeddedBrowserWebView.dll"), []byte("dll"), 0o600); err != nil {
		return err
	}
	data := make([]byte, 96)
	data[0], data[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(data[0x3c:], 0x40)
	copy(data[0x40:], []byte{'P', 'E', 0, 0})
	binary.LittleEndian.PutUint16(data[0x44:], machine)
	return os.WriteFile(filepath.Join(runtimeDirectory, "msedgewebview2.exe"), data, 0o700)
}
