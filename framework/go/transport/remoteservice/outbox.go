package remoteservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

type MemoryOutboxOptions struct {
	Clock frameworkclock.Clock
}

type OutboxStore interface {
	Enqueue(context.Context, OutboxEntry) (OutboxEntry, error)
	Get(context.Context, string) (OutboxEntry, error)
	ListReady(context.Context, time.Time, int) ([]OutboxEntry, error)
	MarkAttempt(context.Context, string, time.Time, error) (OutboxEntry, error)
	MarkSucceeded(context.Context, string) (OutboxEntry, error)
	MarkTerminal(context.Context, string, error) (OutboxEntry, error)
}

type MemoryOutbox struct {
	mu      sync.RWMutex
	clock   frameworkclock.Clock
	entries map[string]OutboxEntry
	order   []string
}

func NewMemoryOutbox(options MemoryOutboxOptions) *MemoryOutbox {
	clock := options.Clock
	if clock == nil {
		clock = frameworkclock.System{}
	}
	return &MemoryOutbox{
		clock:   clock,
		entries: make(map[string]OutboxEntry),
	}
}

func (store *MemoryOutbox) Enqueue(ctx context.Context, entry OutboxEntry) (OutboxEntry, error) {
	if store == nil || ctx == nil || ctx.Err() != nil {
		return OutboxEntry{}, ErrInvalidRequest
	}
	if err := validateOutboxEntry(entry); err != nil {
		return OutboxEntry{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.entries[entry.ID]; exists {
		return OutboxEntry{}, ErrInvalidRequest
	}
	now := store.clock.Now().UTC()
	if entry.Status == "" {
		entry.Status = OutboxStatusPending
	}
	if entry.NextAt.IsZero() {
		entry.NextAt = now
	}
	entry.CreatedAt = now
	entry.UpdatedAt = now
	entry.Request.Headers = cloneHeader(entry.Request.Headers)
	entry.DependsOn = append([]string(nil), entry.DependsOn...)
	store.entries[entry.ID] = cloneOutboxEntry(entry)
	store.order = append(store.order, entry.ID)
	return cloneOutboxEntry(entry), nil
}

func (store *MemoryOutbox) Get(ctx context.Context, id string) (OutboxEntry, error) {
	if store == nil || ctx == nil || ctx.Err() != nil || strings.TrimSpace(id) == "" {
		return OutboxEntry{}, ErrInvalidRequest
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	entry, exists := store.entries[id]
	if !exists {
		return OutboxEntry{}, ErrOutboxNotFound
	}
	return cloneOutboxEntry(entry), nil
}

func (store *MemoryOutbox) ListReady(ctx context.Context, now time.Time, limit int) ([]OutboxEntry, error) {
	if store == nil || ctx == nil || ctx.Err() != nil || limit < 1 {
		return nil, ErrInvalidRequest
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	ready := make([]OutboxEntry, 0, min(limit, len(store.entries)))
	for _, id := range store.order {
		entry := store.entries[id]
		if len(ready) == limit {
			break
		}
		if entry.Status != OutboxStatusPending && entry.Status != OutboxStatusRetrying {
			continue
		}
		if entry.NextAt.After(now) || !store.dependenciesSatisfied(entry) {
			continue
		}
		ready = append(ready, cloneOutboxEntry(entry))
	}
	sort.SliceStable(ready, func(left, right int) bool {
		if ready[left].NextAt.Equal(ready[right].NextAt) {
			return ready[left].CreatedAt.Before(ready[right].CreatedAt)
		}
		return ready[left].NextAt.Before(ready[right].NextAt)
	})
	return ready, nil
}

func (store *MemoryOutbox) MarkAttempt(ctx context.Context, id string, nextAt time.Time, lastErr error) (OutboxEntry, error) {
	if store == nil || ctx == nil || ctx.Err() != nil || strings.TrimSpace(id) == "" {
		return OutboxEntry{}, ErrInvalidRequest
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, exists := store.entries[id]
	if !exists {
		return OutboxEntry{}, ErrOutboxNotFound
	}
	entry.Attempts++
	entry.Status = OutboxStatusRetrying
	entry.NextAt = nextAt
	entry.LastError = errorText(lastErr)
	entry.UpdatedAt = store.clock.Now().UTC()
	store.entries[id] = entry
	return cloneOutboxEntry(entry), nil
}

func (store *MemoryOutbox) MarkSucceeded(ctx context.Context, id string) (OutboxEntry, error) {
	return store.markFinal(ctx, id, OutboxStatusSucceeded, nil)
}

func (store *MemoryOutbox) MarkTerminal(ctx context.Context, id string, err error) (OutboxEntry, error) {
	return store.markFinal(ctx, id, OutboxStatusTerminal, err)
}

func (store *MemoryOutbox) markFinal(ctx context.Context, id string, status OutboxStatus, err error) (OutboxEntry, error) {
	if store == nil || ctx == nil || ctx.Err() != nil || strings.TrimSpace(id) == "" {
		return OutboxEntry{}, ErrInvalidRequest
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, exists := store.entries[id]
	if !exists {
		return OutboxEntry{}, ErrOutboxNotFound
	}
	entry.Attempts++
	entry.Status = status
	entry.LastError = errorText(err)
	entry.UpdatedAt = store.clock.Now().UTC()
	store.entries[id] = entry
	return cloneOutboxEntry(entry), nil
}

func (store *MemoryOutbox) dependenciesSatisfied(entry OutboxEntry) bool {
	for _, dependencyID := range entry.DependsOn {
		dependency, exists := store.entries[dependencyID]
		if !exists || dependency.Status != OutboxStatusSucceeded {
			return false
		}
	}
	return true
}

type PayloadResolver interface {
	ResolvePayload(context.Context, PayloadRef) ([]byte, error)
}

type MapPayloadResolver map[string][]byte

func (resolver MapPayloadResolver) ResolvePayload(ctx context.Context, ref PayloadRef) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || strings.TrimSpace(ref.URI) == "" {
		return nil, ErrInvalidRequest
	}
	payload, exists := resolver[ref.URI]
	if !exists {
		return nil, ErrOutboxNotFound
	}
	if ref.SizeBytes >= 0 && int64(len(payload)) != ref.SizeBytes {
		return nil, ErrInvalidRequest
	}
	if ref.SHA256 != "" {
		sum := sha256.Sum256(payload)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), ref.SHA256) {
			return nil, ErrInvalidRequest
		}
	}
	return append([]byte(nil), payload...), nil
}

type OutboxReplayerOptions struct {
	Store      OutboxStore
	Client     *Client
	Resolver   PayloadResolver
	Clock      frameworkclock.Clock
	RetryDelay time.Duration
}

type ReplayOptions struct {
	Limit int
}

type ReplayResult struct {
	Scanned   int
	Succeeded int
	Failed    int
	Terminal  int
}

type OutboxReplayer struct {
	store      OutboxStore
	client     *Client
	resolver   PayloadResolver
	clock      frameworkclock.Clock
	retryDelay time.Duration
}

func NewOutboxReplayer(options OutboxReplayerOptions) *OutboxReplayer {
	clock := options.Clock
	if clock == nil {
		clock = frameworkclock.System{}
	}
	retryDelay := options.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Minute
	}
	return &OutboxReplayer{
		store: options.Store, client: options.Client, resolver: options.Resolver,
		clock: clock, retryDelay: retryDelay,
	}
}

func (replayer *OutboxReplayer) ReplayReady(ctx context.Context, options ReplayOptions) (ReplayResult, error) {
	if replayer == nil || replayer.store == nil || replayer.client == nil || ctx == nil || ctx.Err() != nil {
		return ReplayResult{}, ErrInvalidRequest
	}
	limit := options.Limit
	if limit < 1 {
		limit = 1
	}
	entries, err := replayer.store.ListReady(ctx, replayer.clock.Now().UTC(), limit)
	if err != nil {
		return ReplayResult{}, err
	}
	result := ReplayResult{Scanned: len(entries)}
	for _, entry := range entries {
		request, err := replayer.requestForEntry(ctx, entry)
		if err != nil {
			result.Failed++
			_, _ = replayer.store.MarkAttempt(ctx, entry.ID, replayer.clock.Now().UTC().Add(replayer.retryDelay), err)
			continue
		}
		response, err := replayer.client.Do(ctx, request)
		if err != nil {
			result.Failed++
			if terminalRemoteError(err) {
				result.Terminal++
				_, _ = replayer.store.MarkTerminal(ctx, entry.ID, err)
				continue
			}
			_, _ = replayer.store.MarkAttempt(ctx, entry.ID, replayer.clock.Now().UTC().Add(replayer.retryDelay), err)
			continue
		}
		if terminalHTTPStatus(response.StatusCode) {
			result.Terminal++
			_, _ = replayer.store.MarkTerminal(ctx, entry.ID, fmt.Errorf("terminal status=%d", response.StatusCode))
			continue
		}
		result.Succeeded++
		_, _ = replayer.store.MarkSucceeded(ctx, entry.ID)
	}
	return result, nil
}

func (replayer *OutboxReplayer) requestForEntry(ctx context.Context, entry OutboxEntry) (Request, error) {
	var body []byte
	var err error
	if entry.Request.Payload.URI != "" {
		if replayer.resolver == nil {
			return Request{}, ErrInvalidRequest
		}
		body, err = replayer.resolver.ResolvePayload(ctx, entry.Request.Payload)
		if err != nil {
			return Request{}, err
		}
	}
	headers := cloneHeader(entry.Request.Headers)
	if entry.Request.Payload.MediaType != "" && headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", entry.Request.Payload.MediaType)
	}
	return Request{
		Method: entry.Request.Method, URL: entry.Request.URL, Headers: headers, Body: body,
		RequestID: entry.Request.RequestID, IdempotencyKey: entry.Request.IdempotencyKey, Timeout: entry.Request.Timeout,
	}, nil
}

func (checkpoint TransferCheckpoint) ApplyResumeHeaders(headers http.Header) http.Header {
	result := cloneHeader(headers)
	if checkpoint.CompletedBytes > 0 {
		result.Set("Range", fmt.Sprintf("bytes=%d-", checkpoint.CompletedBytes))
		if checkpoint.ETag != "" {
			result.Set("If-Range", checkpoint.ETag)
		} else if checkpoint.LastModified != "" {
			result.Set("If-Range", checkpoint.LastModified)
		}
	}
	return result
}

func validateOutboxEntry(entry OutboxEntry) error {
	if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.Operation) == "" {
		return ErrInvalidRequest
	}
	if strings.TrimSpace(entry.Request.Method) == "" || strings.TrimSpace(entry.Request.URL) == "" {
		return ErrInvalidRequest
	}
	method := strings.ToUpper(strings.TrimSpace(entry.Request.Method))
	if !(ReplayRequest{Method: method, IdempotencyKey: entry.Request.IdempotencyKey}).isReplaySafe() {
		return ErrInvalidRequest
	}
	if err := validatePayloadRef(entry.Request.Payload); err != nil {
		return err
	}
	return nil
}

func validatePayloadRef(ref PayloadRef) error {
	if ref.URI == "" && ref.SHA256 == "" && ref.SizeBytes == 0 && ref.MediaType == "" {
		return nil
	}
	if strings.TrimSpace(ref.URI) == "" || ref.SizeBytes < 0 {
		return ErrInvalidRequest
	}
	if ref.SHA256 != "" && len(ref.SHA256) != 64 {
		return ErrInvalidRequest
	}
	return nil
}

func (request ReplayRequest) isReplaySafe() bool {
	switch strings.ToUpper(strings.TrimSpace(request.Method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return strings.TrimSpace(request.IdempotencyKey) != ""
	}
}

func terminalRemoteError(err error) bool {
	return errors.Is(err, ErrPolicyDenied) || errors.Is(err, ErrInvalidRequest)
}

func terminalHTTPStatus(status int) bool {
	return status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests
}

func cloneOutboxEntry(entry OutboxEntry) OutboxEntry {
	entry.Request.Headers = cloneHeader(entry.Request.Headers)
	entry.DependsOn = append([]string(nil), entry.DependsOn...)
	return entry
}

func cloneHeader(headers http.Header) http.Header {
	if headers == nil {
		return http.Header{}
	}
	return headers.Clone()
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 512 {
		return text[:512]
	}
	return text
}
