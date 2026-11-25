package remoteservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloaderResumesTempFileAndCommitsAtomically(t *testing.T) {
	payload := []byte("hello resumable download")
	expectedHash := sha256HexBytes(payload)
	var rangeHeader string
	var ifRangeHeader string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		rangeHeader = request.Header.Get("Range")
		ifRangeHeader = request.Header.Get("If-Range")
		if rangeHeader != "bytes=6-" {
			t.Fatalf("Range = %q", rangeHeader)
		}
		writer.Header().Set("ETag", `"v1"`)
		writer.Header().Set("Content-Range", "bytes 6-24/25")
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(payload[6:])
	}))
	defer server.Close()

	root := t.TempDir()
	targetPath := filepath.Join(root, "artifact.bin")
	tempPath := filepath.Join(root, "artifact.bin.part")
	if err := os.WriteFile(tempPath, payload[:6], 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(ClientOptions{Policy: policyForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	downloader := NewDownloader(DownloaderOptions{Client: client})
	result, err := downloader.Download(context.Background(), DownloadRequest{
		URL: server.URL, TargetPath: targetPath, TempPath: tempPath,
		ExpectedSHA256: expectedHash, ETag: `"v1"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Resumed || result.BytesWritten != int64(len(payload)) || result.SHA256 != expectedHash {
		t.Fatalf("download result = %#v", result)
	}
	if rangeHeader != "bytes=6-" || ifRangeHeader != `"v1"` {
		t.Fatalf("续传请求头错误: Range=%q If-Range=%q", rangeHeader, ifRangeHeader)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("target payload = %q", data)
	}
	if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("临时文件应删除，stat err=%v", err)
	}
}

func TestDownloaderHashMismatchKeepsTempAndDoesNotExposeTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("corrupt"))
	}))
	defer server.Close()

	root := t.TempDir()
	targetPath := filepath.Join(root, "artifact.bin")
	tempPath := filepath.Join(root, "artifact.bin.part")
	client, err := NewClient(ClientOptions{Policy: policyForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	downloader := NewDownloader(DownloaderOptions{Client: client})
	_, err = downloader.Download(context.Background(), DownloadRequest{
		URL: server.URL, TargetPath: targetPath, TempPath: tempPath,
		ExpectedSHA256: strings.Repeat("a", 64),
	})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("校验错误 = %v", err)
	}
	if _, err := os.Stat(targetPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("校验失败后目标文件不可见，stat err=%v", err)
	}
	data, err := os.ReadFile(tempPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "corrupt" {
		t.Fatalf("临时文件内容 = %q", data)
	}
}

func TestDownloaderRejectsUnexpectedResumeStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Range") == "" {
			t.Fatalf("expected Range request")
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "full-body")
	}))
	defer server.Close()

	root := t.TempDir()
	targetPath := filepath.Join(root, "artifact.bin")
	tempPath := filepath.Join(root, "artifact.bin.part")
	if err := os.WriteFile(tempPath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(ClientOptions{Policy: policyForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	downloader := NewDownloader(DownloaderOptions{Client: client})
	_, err = downloader.Download(context.Background(), DownloadRequest{
		URL: server.URL, TargetPath: targetPath, TempPath: tempPath,
	})
	if !errors.Is(err, ErrInvalidResumeResponse) {
		t.Fatalf("续传响应错误 = %v", err)
	}
}

func TestDownloaderStreamsBeyondClientBodyLimitWithExplicitMaxBytes(t *testing.T) {
	payload := []byte("payload larger than ordinary response limit")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	root := t.TempDir()
	targetPath := filepath.Join(root, "large.bin")
	tempPath := filepath.Join(root, "large.bin.part")
	client, err := NewClient(ClientOptions{Policy: policyForServer(t, server.URL), MaxBodyBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	downloader := NewDownloader(DownloaderOptions{Client: client})
	result, err := downloader.Download(context.Background(), DownloadRequest{
		URL: server.URL, TargetPath: targetPath, TempPath: tempPath,
		ExpectedSHA256: sha256HexBytes(payload), MaxBytes: int64(len(payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BytesWritten != int64(len(payload)) {
		t.Fatalf("BytesWritten = %d", result.BytesWritten)
	}
}

func sha256HexBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
