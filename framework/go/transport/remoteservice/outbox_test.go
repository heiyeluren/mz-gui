package remoteservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

func TestOutboxEnqueueRequiresIdempotencyKeyForWriteReplay(t *testing.T) {
	store := NewMemoryOutbox(MemoryOutboxOptions{Clock: fixedClock{now: time.Unix(100, 0)}})

	_, err := store.Enqueue(context.Background(), OutboxEntry{
		ID:        "entry-1",
		Operation: "sync-profile",
		Request: ReplayRequest{
			Method: http.MethodPost,
			URL:    "https://api.example.test/v1/profile",
			Payload: PayloadRef{
				URI:       "blob://payloads/profile-1",
				SHA256:    strings.Repeat("a", 64),
				SizeBytes: 12,
				MediaType: "application/json",
			},
		},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("缺少幂等键的写请求错误 = %v", err)
	}
}

func TestMemoryOutboxListsReadyEntriesByTimeAndDependencies(t *testing.T) {
	clock := fixedClock{now: time.Unix(200, 0)}
	store := NewMemoryOutbox(MemoryOutboxOptions{Clock: clock})
	ctx := context.Background()

	mustEnqueue(t, store, OutboxEntry{
		ID:        "later",
		Operation: "sync-later",
		Request:   replayGet("https://api.example.test/later"),
		NextAt:    clock.Now().Add(time.Minute),
	})
	mustEnqueue(t, store, OutboxEntry{
		ID:        "blocked",
		Operation: "sync-blocked",
		Request:   replayGet("https://api.example.test/blocked"),
		DependsOn: []string{"missing-entry"},
	})
	mustEnqueue(t, store, OutboxEntry{
		ID:        "ready-2",
		Operation: "sync-ready-2",
		Request:   replayGet("https://api.example.test/ready-2"),
		NextAt:    clock.Now(),
	})
	mustEnqueue(t, store, OutboxEntry{
		ID:        "ready-1",
		Operation: "sync-ready-1",
		Request:   replayGet("https://api.example.test/ready-1"),
		NextAt:    clock.Now().Add(-time.Second),
	})

	ready, err := store.ListReady(ctx, clock.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := entryIDs(ready); strings.Join(got, ",") != "ready-1,ready-2" {
		t.Fatalf("ready entries = %v", got)
	}
}

func TestOutboxReplayerResolvesPayloadAndMarksSuccess(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Idempotency-Key"); got != "idem-1" {
			t.Errorf("Idempotency-Key = %q", got)
		}
		payload, _ := io.ReadAll(request.Body)
		receivedBody = string(payload)
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()

	clock := fixedClock{now: time.Unix(300, 0)}
	store := NewMemoryOutbox(MemoryOutboxOptions{Clock: clock})
	mustEnqueue(t, store, OutboxEntry{
		ID:        "entry-1",
		Operation: "upload",
		Request: ReplayRequest{
			Method:         http.MethodPost,
			URL:            server.URL,
			IdempotencyKey: "idem-1",
			Payload: PayloadRef{
				URI:       "blob://payloads/upload-1",
				SHA256:    sha256Hex([]byte("payload")),
				SizeBytes: int64(len("payload")),
				MediaType: "text/plain",
			},
		},
		NextAt: clock.Now(),
	})
	client, err := NewClient(ClientOptions{Policy: policyForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	replayer := NewOutboxReplayer(OutboxReplayerOptions{
		Store:  store,
		Client: client,
		Resolver: MapPayloadResolver{
			"blob://payloads/upload-1": []byte("payload"),
		},
		Clock: clock,
	})

	result, err := replayer.ReplayReady(context.Background(), ReplayOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 1 || result.Failed != 0 || receivedBody != "payload" {
		t.Fatalf("replay result=%#v received=%q", result, receivedBody)
	}
	entry, err := store.Get(context.Background(), "entry-1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Status != OutboxStatusSucceeded || entry.Attempts != 1 {
		t.Fatalf("entry 状态 = %#v", entry)
	}
}

func TestResumeCheckpointBuildsRangeHeaders(t *testing.T) {
	headers := http.Header{"Accept": []string{"application/octet-stream"}}
	checkpoint := TransferCheckpoint{
		ID:             "download-1",
		URL:            "https://api.example.test/files/report",
		CompletedBytes: 4096,
		ETag:           `"etag-1"`,
	}

	resumed := checkpoint.ApplyResumeHeaders(headers)
	if resumed.Get("Range") != "bytes=4096-" {
		t.Fatalf("Range = %q", resumed.Get("Range"))
	}
	if resumed.Get("If-Range") != `"etag-1"` {
		t.Fatalf("If-Range = %q", resumed.Get("If-Range"))
	}
	if headers.Get("Range") != "" {
		t.Fatalf("ApplyResumeHeaders 不应修改输入 headers")
	}
}

func mustEnqueue(t *testing.T, store *MemoryOutbox, entry OutboxEntry) {
	t.Helper()
	if _, err := store.Enqueue(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
}

func replayGet(rawURL string) ReplayRequest {
	return ReplayRequest{Method: http.MethodGet, URL: rawURL}
}

func entryIDs(entries []OutboxEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

func (clock fixedClock) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- clock.now
	return ch
}

func (clock fixedClock) NewTicker(time.Duration) (frameworkclock.Ticker, error) {
	return fixedTicker{ch: make(chan time.Time)}, nil
}

type fixedTicker struct {
	ch chan time.Time
}

func (ticker fixedTicker) C() <-chan time.Time {
	return ticker.ch
}

func (ticker fixedTicker) Stop() {}
