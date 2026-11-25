package remoteservice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUploaderCompletesSessionAfterAllChunksUploaded(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(sourcePath, []byte("abcdefgh"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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
	store := NewMemoryUploadSessionStore()
	lifecycle := &recordingUploadLifecycle{completeStatus: http.StatusCreated}
	result, err := uploader.UploadFile(context.Background(), UploadFileRequest{
		Method: http.MethodPut, URL: server.URL, FilePath: sourcePath, ChunkSize: 4,
		SessionID: "session-complete-adapter", SessionStore: store, SessionLifecycle: lifecycle,
		RequestID: "req-complete", IdempotencyKey: "idem-complete",
		LifecycleMetadata: map[string]string{"provider": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.SessionCompleted || result.SessionAborted || result.LifecycleStatusCode != http.StatusCreated {
		t.Fatalf("complete 结果异常: %#v", result)
	}
	if len(lifecycle.completed) != 1 || len(lifecycle.aborted) != 0 {
		t.Fatalf("lifecycle 调用异常: complete=%d abort=%d", len(lifecycle.completed), len(lifecycle.aborted))
	}
	request := lifecycle.completed[0]
	if request.Session.SessionID != "session-complete-adapter" || !request.Session.IsComplete() || request.Session.CompletedBytes() != 8 {
		t.Fatalf("complete session 状态异常: %#v", request.Session)
	}
	if request.RequestID != "req-complete" || request.IdempotencyKey != "idem-complete:complete" || request.Metadata["provider"] != "test" {
		t.Fatalf("complete 请求元数据异常: %#v", request)
	}
}

func TestUploaderAbortsSessionOnChunkFailureWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(sourcePath, []byte("abcdefgh"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
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
	lifecycle := &recordingUploadLifecycle{abortStatus: http.StatusNoContent}
	result, err := uploader.UploadFile(context.Background(), UploadFileRequest{
		Method: http.MethodPut, URL: server.URL, FilePath: sourcePath, ChunkSize: 4,
		SessionID: "session-abort-adapter", SessionStore: NewMemoryUploadSessionStore(), SessionLifecycle: lifecycle,
		AbortSessionOnFailure: true, RequestID: "req-abort", IdempotencyKey: "idem-abort",
	})
	if !errors.Is(err, ErrUploadFailed) {
		t.Fatalf("应保留上传失败语义: result=%#v err=%v", result, err)
	}
	if result.SessionCompleted || !result.SessionAborted || result.LifecycleStatusCode != http.StatusNoContent {
		t.Fatalf("abort 结果异常: %#v", result)
	}
	if len(lifecycle.completed) != 0 || len(lifecycle.aborted) != 1 {
		t.Fatalf("lifecycle 调用异常: complete=%d abort=%d", len(lifecycle.completed), len(lifecycle.aborted))
	}
	request := lifecycle.aborted[0]
	if request.Session.SessionID != "session-abort-adapter" || !request.Session.IsChunkCompleted(0) || request.Session.IsChunkCompleted(1) {
		t.Fatalf("abort session 状态异常: %#v", request.Session)
	}
	if request.IdempotencyKey != "idem-abort:abort" {
		t.Fatalf("abort 幂等键异常: %#v", request)
	}
}

func TestUploaderRejectsLifecycleWithoutResumeSession(t *testing.T) {
	client, err := NewClient(ClientOptions{Policy: Policy{
		AllowedSchemes: map[string]struct{}{"http": {}},
		AllowedHosts:   map[string]struct{}{"127.0.0.1": {}},
		AllowedMethods: map[string]struct{}{http.MethodPut: {}},
		MaxBodyBytes:   1024,
	}})
	if err != nil {
		t.Fatal(err)
	}
	uploader, err := NewUploader(client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = uploader.UploadFile(context.Background(), UploadFileRequest{
		Method: http.MethodPut, URL: "http://127.0.0.1/upload", FilePath: "missing.bin", ChunkSize: 4,
		SessionLifecycle: &recordingUploadLifecycle{},
	})
	if !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("缺少 session store/id 时应拒绝 lifecycle: %v", err)
	}
}

type recordingUploadLifecycle struct {
	completeStatus int
	abortStatus    int
	completed      []UploadSessionLifecycleRequest
	aborted        []UploadSessionLifecycleRequest
}

func (lifecycle *recordingUploadLifecycle) CompleteUploadSession(ctx context.Context, request UploadSessionLifecycleRequest) (UploadSessionLifecycleResult, error) {
	if ctx == nil {
		return UploadSessionLifecycleResult{}, ErrInvalidUpload
	}
	lifecycle.completed = append(lifecycle.completed, request)
	return UploadSessionLifecycleResult{StatusCode: lifecycle.completeStatus, CompletedAt: time.Unix(100, 0).UTC()}, nil
}

func (lifecycle *recordingUploadLifecycle) AbortUploadSession(ctx context.Context, request UploadSessionLifecycleRequest) (UploadSessionLifecycleResult, error) {
	if ctx == nil {
		return UploadSessionLifecycleResult{}, ErrInvalidUpload
	}
	lifecycle.aborted = append(lifecycle.aborted, request)
	return UploadSessionLifecycleResult{StatusCode: lifecycle.abortStatus, AbortedAt: time.Unix(101, 0).UTC()}, nil
}
