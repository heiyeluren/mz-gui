package remoteservice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUploaderResumesSessionAfterChunkFailure(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(sourcePath, []byte("abcdefghij"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryUploadSessionStore()
	var mu sync.Mutex
	bodies := make([]string, 0, 5)
	ranges := make([]string, 0, 5)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("读取上传 body: %v", err)
		}
		_ = request.Body.Close()
		mu.Lock()
		defer mu.Unlock()
		calls++
		bodies = append(bodies, string(body))
		ranges = append(ranges, request.Header.Get("Content-Range"))
		if calls == 2 {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{Policy: uploadPolicyForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	uploader, err := NewUploader(client)
	if err != nil {
		t.Fatal(err)
	}
	first, err := uploader.UploadFile(context.Background(), UploadFileRequest{
		Method: http.MethodPut, URL: server.URL, FilePath: sourcePath, ChunkSize: 4,
		SessionID: "session-1", SessionStore: store, Timeout: time.Second,
	})
	if !errors.Is(err, ErrUploadFailed) {
		t.Fatalf("首次上传应在第二片失败: result=%#v err=%v", first, err)
	}
	if first.CompletedChunks != 1 || first.UploadedBytes != 4 || first.SkippedChunks != 0 {
		t.Fatalf("首次失败进度异常: %#v", first)
	}
	state, found, err := store.LoadUploadSession(context.Background(), "session-1")
	if err != nil || !found {
		t.Fatalf("应保存 session: found=%v err=%v", found, err)
	}
	if !state.IsChunkCompleted(0) || state.IsChunkCompleted(1) {
		t.Fatalf("session 已完成分片异常: %#v", state.CompletedChunks)
	}

	second, err := uploader.UploadFile(context.Background(), UploadFileRequest{
		Method: http.MethodPut, URL: server.URL, FilePath: sourcePath, ChunkSize: 4,
		SessionID: "session-1", SessionStore: store, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.TotalBytes != 10 || second.CompletedChunks != 3 || second.UploadedBytes != 10 || second.SkippedChunks != 1 {
		t.Fatalf("恢复上传结果异常: %#v", second)
	}
	mu.Lock()
	gotBodies := strings.Join(bodies, "|")
	gotRanges := strings.Join(ranges, "|")
	mu.Unlock()
	if gotBodies != "abcd|efgh|efgh|ij" {
		t.Fatalf("恢复上传不应重复第一片: %q", gotBodies)
	}
	if gotRanges != "bytes 0-3/10|bytes 4-7/10|bytes 4-7/10|bytes 8-9/10" {
		t.Fatalf("恢复上传 range 异常: %q", gotRanges)
	}
}

func TestUploaderRejectsResumeWhenSessionShapeChanges(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(sourcePath, []byte("abcdefgh"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryUploadSessionStore()
	state, err := NewUploadSessionState("session-shape", 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	state.MarkChunkCompleted(UploadChunk{Index: 0, Start: 0, End: 3, Total: 8}, time.Now())
	if err := store.SaveUploadSession(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Fatal("参数漂移时不应发起网络请求")
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{Policy: uploadPolicyForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	uploader, err := NewUploader(client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = uploader.UploadFile(context.Background(), UploadFileRequest{
		Method: http.MethodPut, URL: server.URL, FilePath: sourcePath, ChunkSize: 2,
		SessionID: "session-shape", SessionStore: store,
	})
	if !errors.Is(err, ErrUploadSessionConflict) {
		t.Fatalf("参数漂移应拒绝恢复: %v", err)
	}
}

func TestUploaderSkipsNetworkWhenSessionAlreadyComplete(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(sourcePath, []byte("abcdefgh"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryUploadSessionStore()
	state, err := NewUploadSessionState("session-complete", 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []UploadChunk{
		{Index: 0, Start: 0, End: 3, Total: 8},
		{Index: 1, Start: 4, End: 7, Total: 8},
	} {
		state.MarkChunkCompleted(chunk, time.Now())
	}
	if err := store.SaveUploadSession(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Fatal("session 已完成时不应重复上传")
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{Policy: uploadPolicyForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	uploader, err := NewUploader(client)
	if err != nil {
		t.Fatal(err)
	}
	result, err := uploader.UploadFile(context.Background(), UploadFileRequest{
		Method: http.MethodPut, URL: server.URL, FilePath: sourcePath, ChunkSize: 4,
		SessionID: "session-complete", SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CompletedChunks != 2 || result.SkippedChunks != 2 || result.UploadedBytes != 8 || result.LastStatusCode != 0 {
		t.Fatalf("已完成 session 结果异常: %#v", result)
	}
}
