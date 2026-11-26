package artifact

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
)

func TestDownloaderRestrictsOriginSizeAndDigest(t *testing.T) {
	payload := []byte("verified artifact")
	digest := sha256.Sum256(payload)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", "17")
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	policy := Policy{AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")}, MaxBytes: 32}
	destination := filepath.Join(t.TempDir(), "artifact.bin")
	downloader := Downloader{Client: server.Client()}
	result, err := downloader.Download(context.Background(), server.URL+"/releases/a.bin", destination, hex.EncodeToString(digest[:]), policy)
	if err != nil || result.Bytes != int64(len(payload)) || result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("下载结果: %#v err=%v", result, err)
	}
	stored, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(stored, payload) {
		t.Fatalf("下载文件: %q err=%v", stored, err)
	}
	if _, err := downloader.Download(context.Background(), server.URL+"/releases/a.bin", filepath.Join(t.TempDir(), "bad.bin"), strings.Repeat("0", 64), policy); err == nil {
		t.Fatalf("错误哈希未被拒绝")
	}
	policy.MaxBytes = 4
	if _, err := downloader.Download(context.Background(), server.URL+"/releases/a.bin", filepath.Join(t.TempDir(), "large.bin"), hex.EncodeToString(digest[:]), policy); err == nil {
		t.Fatalf("超限下载未被拒绝")
	}
	policy.AllowedHosts = []string{"example.invalid"}
	if _, err := downloader.Download(context.Background(), server.URL+"/releases/a.bin", filepath.Join(t.TempDir(), "host.bin"), hex.EncodeToString(digest[:]), policy); err == nil {
		t.Fatalf("非白名单来源未被拒绝")
	}
}

func TestDownloaderRequiresPathPrefixSegmentBoundary(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("artifact"))
	}))
	defer server.Close()
	downloader := Downloader{Client: server.Client()}
	policy := Policy{AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")}, PathPrefix: "/skills", MaxBytes: 32}
	if _, err := downloader.Fetch(context.Background(), server.URL+"/skills/catalog.json", policy); err != nil {
		t.Fatalf("合法路径前缀被拒绝: %v", err)
	}
	if _, err := downloader.Fetch(context.Background(), server.URL+"/skills-evil/catalog.json", policy); err == nil {
		t.Fatal("相似字符串路径越过白名单边界")
	}
}

func TestDownloaderAllowsAnyDeclaredPathPrefix(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	downloader := Downloader{Client: server.Client()}
	policy := Policy{
		AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")},
		PathPrefix:   "/api/v1",
		PathPrefixes: []string{"/packages/asset-hub/"},
		MaxBytes:     64,
	}
	if _, err := downloader.Fetch(context.Background(), server.URL+"/api/v1/assets/download", policy); err != nil {
		t.Fatalf("主路径前缀被拒绝: %v", err)
	}
	if _, err := downloader.Fetch(context.Background(), server.URL+"/packages/asset-hub/skill/demo.tar.gz", policy); err != nil {
		t.Fatalf("附加路径前缀被拒绝: %v", err)
	}
	if _, err := downloader.Fetch(context.Background(), server.URL+"/packages/asset-hub-evil/skill/demo.tar.gz", policy); err == nil {
		t.Fatal("附加路径前缀缺少段边界校验")
	}
}

func TestDownloaderRequiresSignedManifestSizeBeforeCommit(t *testing.T) {
	payload := []byte("signed installer")
	digest := sha256.Sum256(payload)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	downloader := Downloader{Client: server.Client()}
	policy := Policy{
		AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")},
		MaxBytes:     64, ExpectedBytes: int64(len(payload)) + 1,
	}
	destination := filepath.Join(t.TempDir(), "installer.bin")
	if _, err := downloader.Download(context.Background(), server.URL+"/installer.bin", destination, hex.EncodeToString(digest[:]), policy); err == nil {
		t.Fatal("大小与签名清单不一致的制品未被拒绝")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("失败制品不应提交到目标路径: %v", err)
	}
	policy.ExpectedBytes = int64(len(payload))
	if _, err := downloader.Download(context.Background(), server.URL+"/installer.bin", destination, hex.EncodeToString(digest[:]), policy); err != nil {
		t.Fatalf("合法制品下载失败: %v", err)
	}
}

func TestDownloaderFetchesBoundedMetadata(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"version":"1.0.0"}`))
	}))
	defer server.Close()
	downloader := Downloader{Client: server.Client()}
	policy := Policy{AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")}, MaxBytes: 64}
	data, err := downloader.Fetch(context.Background(), server.URL+"/manifest.json", policy)
	if err != nil || string(data) != `{"version":"1.0.0"}` {
		t.Fatalf("读取元数据: %q err=%v", data, err)
	}
	policy.MaxBytes = 4
	if _, err := downloader.Fetch(context.Background(), server.URL+"/manifest.json", policy); err == nil {
		t.Fatalf("超限元数据未被拒绝")
	}
}

func TestDownloaderReturnsStableHTTPStatusWithoutLeakingURL(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "private upstream detail", http.StatusUnauthorized)
	}))
	defer server.Close()
	_, err := (Downloader{Client: server.Client()}).DownloadAndHash(context.Background(), server.URL+"/secret/path", filepath.Join(t.TempDir(), "artifact"), 0,
		Policy{AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")}, MaxBytes: 64})
	statusCode, ok := HTTPStatusCode(err)
	if !ok || statusCode != http.StatusUnauthorized || strings.Contains(err.Error(), "secret/path") || strings.Contains(err.Error(), "private upstream") {
		t.Fatalf("HTTP 状态错误不稳定或泄漏详情: code=%d ok=%v err=%v", statusCode, ok, err)
	}
}

func TestDownloaderDownloadAndHashRequiresExactSize(t *testing.T) {
	payload := []byte("PK\x03\x04android-package")
	digest := sha256.Sum256(payload)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	downloader := Downloader{Client: server.Client()}
	policy := Policy{AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")}, MaxBytes: 64}
	destination := filepath.Join(t.TempDir(), "package.apk")
	result, err := downloader.DownloadAndHash(context.Background(), server.URL+"/signed/package.apk", destination, int64(len(payload)), policy)
	if err != nil || result.Bytes != int64(len(payload)) || result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("APK 下载结果: %#v err=%v", result, err)
	}
	if _, err := downloader.DownloadAndHash(context.Background(), server.URL+"/signed/package.apk", filepath.Join(t.TempDir(), "wrong.apk"), int64(len(payload))+1, policy); err == nil {
		t.Fatal("大小不一致的制品未被拒绝")
	}
}

func TestDownloaderDownloadAndHashSupportsBoundedAuthorizedResponse(t *testing.T) {
	payload := []byte("asset-hub-package")
	digest := sha256.Sum256(payload)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	downloader := Downloader{Client: server.Client(), Authorize: func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer token")
		return nil
	}}
	policy := Policy{AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")}, MaxBytes: 64}
	destination := filepath.Join(t.TempDir(), "package.bin")
	result, err := downloader.DownloadAndHash(context.Background(), server.URL+"/api/v1/assets/download", destination, 0, policy)
	if err != nil || result.Bytes != int64(len(payload)) || result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("受控下载结果: %#v err=%v", result, err)
	}
	downloader.Authorize = func(context.Context, *http.Request) error { return errors.New("token unavailable") }
	if _, err := downloader.DownloadAndHash(context.Background(), server.URL+"/api/v1/assets/download", filepath.Join(t.TempDir(), "failed.bin"), 0, policy); err == nil {
		t.Fatal("授权失败未阻止请求")
	}
}

func TestDownloaderDownloadAndHashRejectsStreamingResponseOverLimit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.(http.Flusher).Flush()
		_, _ = writer.Write(bytes.Repeat([]byte("x"), 65))
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "oversized.bin")
	_, err := (Downloader{Client: server.Client()}).DownloadAndHash(context.Background(), server.URL+"/artifact", destination, 0,
		Policy{AllowedHosts: []string{strings.TrimPrefix(server.URL, "https://")}, MaxBytes: 64})
	if err == nil {
		t.Fatal("无 Content-Length 的超限制品未被拒绝")
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("超限制品不应发布到目标路径: %v", statErr)
	}
}

func TestCopyAndVerifyPublishesExactRegularArtifactIdempotently(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.zip")
	destination := filepath.Join(root, "packages", "demo.zip")
	payload := []byte("trusted-package")
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	expected := hex.EncodeToString(digest[:])

	result, err := CopyAndVerify(context.Background(), source, destination, expected, int64(len(payload)), int64(len(payload)))
	if err != nil || !result.Applied || result.SHA256 != expected || result.Bytes != int64(len(payload)) {
		t.Fatalf("首次收编制品失败: %#v err=%v", result, err)
	}
	info, err := os.Lstat(destination)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("目标制品不是普通文件: mode=%v err=%v", info.Mode(), err)
	}
	testkit.AssertFileMode(t, destination, 0o600)
	replayed, err := CopyAndVerify(context.Background(), source, destination, expected, int64(len(payload)), int64(len(payload)))
	if err != nil || replayed.Applied {
		t.Fatalf("制品收编幂等重放错误: %#v err=%v", replayed, err)
	}
}

func TestCopyAndVerifyRejectsSymlinkDigestSizeAndTargetConflict(t *testing.T) {
	testkit.RequireSymlink(t)
	root := t.TempDir()
	source := filepath.Join(root, "source.zip")
	payload := []byte("trusted-package")
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	expected := hex.EncodeToString(digest[:])

	link := filepath.Join(root, "source-link.zip")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if _, err := CopyAndVerify(context.Background(), link, filepath.Join(root, "link-copy.zip"), expected, int64(len(payload)), 1024); !errors.Is(err, ErrLocalArtifactInvalid) {
		t.Fatalf("软链接来源未被拒绝: %v", err)
	}
	if _, err := CopyAndVerify(context.Background(), source, filepath.Join(root, "digest.zip"), strings.Repeat("0", 64), int64(len(payload)), 1024); !errors.Is(err, ErrLocalArtifactDigest) {
		t.Fatalf("错误摘要未被拒绝: %v", err)
	}
	if _, err := CopyAndVerify(context.Background(), source, filepath.Join(root, "size.zip"), expected, int64(len(payload))+1, 1024); !errors.Is(err, ErrLocalArtifactSize) {
		t.Fatalf("错误大小未被拒绝: %v", err)
	}
	destination := filepath.Join(root, "conflict.zip")
	if err := os.WriteFile(destination, []byte("different"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CopyAndVerify(context.Background(), source, destination, expected, int64(len(payload)), 1024); !errors.Is(err, ErrLocalArtifactConflict) {
		t.Fatalf("已有冲突目标未被拒绝: %v", err)
	}
}

func TestExtractTarGzipAcceptsRegularTree(t *testing.T) {
	archive := createArchive(t, []archiveEntry{
		{name: ".", mode: 0o755, typeflag: tar.TypeDir},
		{name: "release/", mode: 0o755, typeflag: tar.TypeDir},
		{name: "release/bin/runner", body: "runner", mode: 0o755, typeflag: tar.TypeReg},
		{name: "release/config.yaml", body: "enabled: true\n", mode: 0o644, typeflag: tar.TypeReg},
	})
	destination := filepath.Join(t.TempDir(), "extracted")
	result, err := ExtractTarGzip(archive, destination, Policy{MaxEntries: 10, MaxExtractedBytes: 1024, MaxPathBytes: 256})
	if err != nil || result.Entries != 4 || result.Bytes != 20 {
		t.Fatalf("解压结果: %#v err=%v", result, err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "release", "config.yaml"))
	if err != nil || string(content) != "enabled: true\n" {
		t.Fatalf("解压内容: %q err=%v", content, err)
	}
}

func TestExtractTarGzipRejectsTraversalLinksAndLimits(t *testing.T) {
	tests := []struct {
		name    string
		entries []archiveEntry
		policy  Policy
	}{
		{name: "traversal", entries: []archiveEntry{{name: "../escape", body: "bad", mode: 0o644, typeflag: tar.TypeReg}}},
		{name: "absolute", entries: []archiveEntry{{name: "/escape", body: "bad", mode: 0o644, typeflag: tar.TypeReg}}},
		{name: "symlink", entries: []archiveEntry{{name: "link", linkname: "target", mode: 0o777, typeflag: tar.TypeSymlink}}},
		{name: "hardlink", entries: []archiveEntry{{name: "link", linkname: "target", mode: 0o777, typeflag: tar.TypeLink}}},
		{name: "special", entries: []archiveEntry{{name: "pipe", mode: 0o600, typeflag: tar.TypeFifo}}},
		{name: "bytes", entries: []archiveEntry{{name: "large", body: "12345", mode: 0o600, typeflag: tar.TypeReg}}, policy: Policy{MaxEntries: 10, MaxExtractedBytes: 4, MaxPathBytes: 256}},
		{name: "entries", entries: []archiveEntry{{name: "a", body: "a", mode: 0o600, typeflag: tar.TypeReg}, {name: "b", body: "b", mode: 0o600, typeflag: tar.TypeReg}}, policy: Policy{MaxEntries: 1, MaxExtractedBytes: 10, MaxPathBytes: 256}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := test.policy
			if policy.MaxEntries == 0 {
				policy = Policy{MaxEntries: 10, MaxExtractedBytes: 1024, MaxPathBytes: 256}
			}
			archive := createArchive(t, test.entries)
			if _, err := ExtractTarGzip(archive, filepath.Join(t.TempDir(), "out"), policy); err == nil {
				t.Fatalf("危险归档未被拒绝")
			}
		})
	}
}

func TestExtractZIPAcceptsRegularFilesAndRejectsUnsafeMembers(t *testing.T) {
	valid := createZIP(t, map[string]zipFixture{"bin/ffmpeg": {body: "binary", mode: 0o755}, "README": {body: "release", mode: 0o644}})
	destination := filepath.Join(t.TempDir(), "valid")
	result, err := ExtractZIP(valid, destination, Policy{MaxEntries: 4, MaxExtractedBytes: 64, MaxPathBytes: 128})
	if err != nil || result.Entries != 2 || result.Bytes != 13 {
		t.Fatalf("ZIP 解压: %#v err=%v", result, err)
	}
	if content, err := os.ReadFile(filepath.Join(destination, "bin", "ffmpeg")); err != nil || string(content) != "binary" {
		t.Fatalf("ZIP 文件: %q err=%v", content, err)
	}
	unsafe := []map[string]zipFixture{
		{"../escape": {body: "bad", mode: 0o600}},
		{"/absolute": {body: "bad", mode: 0o600}},
		{"link": {body: "target", mode: os.ModeSymlink | 0o777}},
		{"large": {body: "12345", mode: 0o600}},
	}
	for index, fixture := range unsafe {
		policy := Policy{MaxEntries: 4, MaxExtractedBytes: 64, MaxPathBytes: 128}
		if index == len(unsafe)-1 {
			policy.MaxExtractedBytes = 4
		}
		if _, err := ExtractZIP(createZIP(t, fixture), filepath.Join(t.TempDir(), "unsafe"), policy); err == nil {
			t.Fatalf("危险 ZIP %d 未被拒绝", index)
		}
	}
}

func TestInspectArchiveValidatesContentWithoutExtracting(t *testing.T) {
	policy := Policy{MaxEntries: 4, MaxExtractedBytes: 64, MaxPathBytes: 128}
	zipPath := createZIP(t, map[string]zipFixture{"package/README": {body: "release", mode: 0o644}})
	result, err := InspectArchive(zipPath, ".zip", policy)
	if err != nil || result.Entries != 1 || result.Bytes != 7 {
		t.Fatalf("ZIP 检查结果: %#v err=%v", result, err)
	}
	tarPath := createArchive(t, []archiveEntry{{name: "package/README", body: "release", mode: 0o644, typeflag: tar.TypeReg}})
	result, err = InspectArchive(tarPath, ".tar.gz", policy)
	if err != nil || result.Entries != 1 || result.Bytes != 7 {
		t.Fatalf("tar.gz 检查结果: %#v err=%v", result, err)
	}
	unsafe := createZIP(t, map[string]zipFixture{"../escape": {body: "bad", mode: 0o600}})
	if _, err := InspectArchive(unsafe, ".zip", policy); err == nil {
		t.Fatal("路径穿越 ZIP 未被检查器拒绝")
	}
	if _, err := InspectArchive(zipPath, ".rar", policy); err == nil {
		t.Fatal("未知归档格式未被检查器拒绝")
	}
}

func TestInspectTarGzipAllowsRootDirectoryEntry(t *testing.T) {
	archive := createArchive(t, []archiveEntry{
		{name: ".", mode: 0o755, typeflag: tar.TypeDir},
		{name: "./references", mode: 0o755, typeflag: tar.TypeDir},
		{name: "./references/naming-patterns.md", body: "naming", mode: 0o644, typeflag: tar.TypeReg},
		{name: "./SKILL.md", body: "skill", mode: 0o644, typeflag: tar.TypeReg},
	})
	result, err := InspectArchive(archive, ".tar.gz", Policy{MaxEntries: 8, MaxExtractedBytes: 64, MaxPathBytes: 128})
	if err != nil || result.Bytes != 11 {
		t.Fatalf("包含根目录项的 tar.gz 应可检查通过: result=%#v err=%v", result, err)
	}
}

type archiveEntry struct {
	name, body, linkname string
	mode                 int64
	typeflag             byte
}

func createArchive(t *testing.T, entries []archiveEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.tar.gz")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Linkname: entry.linkname, Mode: entry.mode, Typeflag: entry.typeflag, Size: int64(len(entry.body))}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.body != "" {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

type zipFixture struct {
	body string
	mode os.FileMode
}

func createZIP(t *testing.T, entries map[string]zipFixture) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "fixture.zip")
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, fixture := range entries {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(fixture.mode)
		member, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write([]byte(fixture.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}
