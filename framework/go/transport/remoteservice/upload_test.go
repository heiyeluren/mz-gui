package remoteservice

import (
	"context"
	"errors"
	"fmt"
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

func TestPlanUploadChunksBuildsStableContentRanges(t *testing.T) {
	chunks, err := PlanUploadChunks(10, 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bytes 0-3/10", "bytes 4-7/10", "bytes 8-9/10"}
	if len(chunks) != len(want) {
		t.Fatalf("分片数量 = %d", len(chunks))
	}
	for index, chunk := range chunks {
		if chunk.Index != index || chunk.ContentRange() != want[index] {
			t.Fatalf("分片 %d 异常: %#v range=%s", index, chunk, chunk.ContentRange())
		}
	}
	if _, err := PlanUploadChunks(0, 4); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("空文件应拒绝: %v", err)
	}
	if _, err := PlanUploadChunks(10, 0); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("非法 chunk size 应拒绝: %v", err)
	}
}

func TestUploaderUploadsFileChunksWithContentRangeAndRateGate(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(sourcePath, []byte("abcdefghij"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gate recordingUploadGate
	var mu sync.Mutex
	received := make([]string, 0, 3)
	ranges := make([]string, 0, 3)
	keys := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("读取上传 body: %v", err)
		}
		_ = request.Body.Close()
		mu.Lock()
		defer mu.Unlock()
		received = append(received, string(body))
		ranges = append(ranges, request.Header.Get("Content-Range"))
		keys = append(keys, request.Header.Get("Idempotency-Key"))
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
	result, err := uploader.UploadFile(context.Background(), UploadFileRequest{
		Method: http.MethodPut, URL: server.URL, FilePath: sourcePath, ChunkSize: 4,
		IdempotencyKey: "upload-1", RateGate: &gate, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalBytes != 10 || result.UploadedBytes != 10 || result.CompletedChunks != 3 {
		t.Fatalf("上传结果异常: %#v", result)
	}
	if strings.Join(received, "|") != "abcd|efgh|ij" {
		t.Fatalf("分片 body 异常: %#v", received)
	}
	if strings.Join(ranges, "|") != "bytes 0-3/10|bytes 4-7/10|bytes 8-9/10" {
		t.Fatalf("Content-Range 异常: %#v", ranges)
	}
	if strings.Join(keys, "|") != "upload-1:chunk-0|upload-1:chunk-1|upload-1:chunk-2" {
		t.Fatalf("幂等键异常: %#v", keys)
	}
	if got := gate.Ranges(); strings.Join(got, "|") != strings.Join(ranges, "|") {
		t.Fatalf("rate gate 调用异常: %#v", got)
	}
}

func TestUploaderStopsOnChunkFailure(t *testing.T) {
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
	result, err := uploader.UploadFile(context.Background(), UploadFileRequest{
		Method: http.MethodPut, URL: server.URL, FilePath: sourcePath, ChunkSize: 4,
	})
	if !errors.Is(err, ErrUploadFailed) {
		t.Fatalf("上传失败错误异常: result=%#v err=%v", result, err)
	}
	if result.CompletedChunks != 1 || result.UploadedBytes != 4 {
		t.Fatalf("失败进度异常: %#v", result)
	}
}

func uploadPolicyForServer(t *testing.T, rawURL string) Policy {
	t.Helper()
	policy := policyForServer(t, rawURL)
	policy.AllowedMethods[http.MethodPut] = struct{}{}
	policy.AllowedHeaders["content-range"] = struct{}{}
	return policy
}

type recordingUploadGate struct {
	mu     sync.Mutex
	ranges []string
}

func (gate *recordingUploadGate) WaitChunk(ctx context.Context, chunk UploadChunk) error {
	if ctx == nil {
		return fmt.Errorf("context nil")
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.ranges = append(gate.ranges, chunk.ContentRange())
	return nil
}

func (gate *recordingUploadGate) Ranges() []string {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return append([]string(nil), gate.ranges...)
}
