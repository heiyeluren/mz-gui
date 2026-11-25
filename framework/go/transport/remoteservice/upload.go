package remoteservice

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type UploadChunk struct {
	Index int
	Start int64
	End   int64
	Total int64
}

func (chunk UploadChunk) Size() int64 {
	if chunk.End < chunk.Start {
		return 0
	}
	return chunk.End - chunk.Start + 1
}

func (chunk UploadChunk) ContentRange() string {
	return fmt.Sprintf("bytes %d-%d/%d", chunk.Start, chunk.End, chunk.Total)
}

func PlanUploadChunks(totalBytes, chunkSize int64) ([]UploadChunk, error) {
	if totalBytes <= 0 || chunkSize <= 0 {
		return nil, ErrInvalidUpload
	}
	chunks := make([]UploadChunk, 0, (totalBytes+chunkSize-1)/chunkSize)
	for start := int64(0); start < totalBytes; start += chunkSize {
		end := start + chunkSize - 1
		if end >= totalBytes {
			end = totalBytes - 1
		}
		chunks = append(chunks, UploadChunk{Index: len(chunks), Start: start, End: end, Total: totalBytes})
	}
	return chunks, nil
}

type UploadRateGate interface {
	WaitChunk(context.Context, UploadChunk) error
}

type UploadFileRequest struct {
	Method                string
	URL                   string
	FilePath              string
	Headers               http.Header
	RequestID             string
	IdempotencyKey        string
	ChunkSize             int64
	Timeout               time.Duration
	RateGate              UploadRateGate
	SessionID             string
	SessionStore          UploadSessionStore
	SessionLifecycle      UploadSessionLifecycle
	AbortSessionOnFailure bool
	LifecycleMetadata     map[string]string
}

type UploadResult struct {
	TotalBytes          int64
	UploadedBytes       int64
	TotalChunks         int
	CompletedChunks     int
	SkippedChunks       int
	LastStatusCode      int
	SessionID           string
	SessionCompleted    bool
	SessionAborted      bool
	LifecycleStatusCode int
}

type Uploader struct {
	client *Client
}

func NewUploader(client *Client) (*Uploader, error) {
	if client == nil {
		return nil, ErrInvalidUpload
	}
	return &Uploader{client: client}, nil
}

func (uploader *Uploader) UploadFile(ctx context.Context, request UploadFileRequest) (UploadResult, error) {
	if uploader == nil || uploader.client == nil || ctx == nil || request.URL == "" || request.FilePath == "" || request.ChunkSize <= 0 {
		return UploadResult{}, ErrInvalidUpload
	}
	resumeEnabled := request.SessionID != "" || request.SessionStore != nil
	if resumeEnabled && (request.SessionID == "" || request.SessionStore == nil) {
		return UploadResult{}, ErrInvalidUpload
	}
	if request.SessionLifecycle != nil && !resumeEnabled {
		return UploadResult{}, ErrInvalidUpload
	}
	method := request.Method
	if method == "" {
		method = http.MethodPut
	}
	info, err := os.Stat(request.FilePath)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return UploadResult{}, ErrInvalidUpload
	}
	chunks, err := PlanUploadChunks(info.Size(), request.ChunkSize)
	if err != nil {
		return UploadResult{}, err
	}
	file, err := os.Open(request.FilePath)
	if err != nil {
		return UploadResult{}, err
	}
	defer file.Close()
	result := UploadResult{TotalBytes: info.Size(), TotalChunks: len(chunks), SessionID: request.SessionID}
	sessionState, err := uploader.uploadSessionState(ctx, request, info.Size(), len(chunks))
	if err != nil {
		return result, err
	}
	if resumeEnabled {
		result.CompletedChunks = len(sessionState.CompletedChunks)
		result.SkippedChunks = len(sessionState.CompletedChunks)
		result.UploadedBytes = sessionState.CompletedBytes()
		if sessionState.IsComplete() {
			return result, nil
		}
	}
	for _, chunk := range chunks {
		if resumeEnabled && sessionState.IsChunkCompleted(chunk.Index) {
			continue
		}
		if request.RateGate != nil {
			if err := request.RateGate.WaitChunk(ctx, chunk); err != nil {
				return result, err
			}
		}
		payload := make([]byte, chunk.Size())
		if _, err := file.ReadAt(payload, chunk.Start); err != nil && err != io.EOF {
			return result, err
		}
		headers := request.Headers.Clone()
		if headers == nil {
			headers = http.Header{}
		}
		headers.Set("Content-Range", chunk.ContentRange())
		idempotencyKey := request.IdempotencyKey
		if idempotencyKey != "" {
			idempotencyKey = fmt.Sprintf("%s:chunk-%d", idempotencyKey, chunk.Index)
		}
		response, err := uploader.client.Do(ctx, Request{
			Method: method, URL: request.URL, Headers: headers, Body: payload,
			RequestID: request.RequestID, IdempotencyKey: idempotencyKey, Timeout: request.Timeout,
		})
		if err != nil {
			return result, err
		}
		result.LastStatusCode = response.StatusCode
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			result = uploader.abortUploadSessionOnFailure(ctx, request, sessionState, result)
			return result, fmt.Errorf("%w: chunk=%d status=%d", ErrUploadFailed, chunk.Index, response.StatusCode)
		}
		if resumeEnabled {
			sessionState.MarkChunkCompleted(chunk, time.Now())
			if err := request.SessionStore.SaveUploadSession(ctx, sessionState); err != nil {
				return result, err
			}
		}
		result.CompletedChunks++
		result.UploadedBytes += chunk.Size()
	}
	if request.SessionLifecycle != nil {
		lifecycleResult, err := request.SessionLifecycle.CompleteUploadSession(ctx, buildUploadLifecycleRequest(sessionState, request, "complete"))
		if err != nil {
			return result, err
		}
		result.SessionCompleted = true
		result.LifecycleStatusCode = lifecycleResult.StatusCode
	}
	return result, nil
}

func (uploader *Uploader) abortUploadSessionOnFailure(ctx context.Context, request UploadFileRequest, state UploadSessionState, result UploadResult) UploadResult {
	if request.SessionLifecycle == nil || !request.AbortSessionOnFailure {
		return result
	}
	lifecycleResult, err := request.SessionLifecycle.AbortUploadSession(ctx, buildUploadLifecycleRequest(state, request, "abort"))
	if err != nil {
		return result
	}
	result.SessionAborted = true
	result.LifecycleStatusCode = lifecycleResult.StatusCode
	return result
}

func (uploader *Uploader) uploadSessionState(ctx context.Context, request UploadFileRequest, totalBytes int64, totalChunks int) (UploadSessionState, error) {
	if request.SessionID == "" && request.SessionStore == nil {
		return UploadSessionState{}, nil
	}
	state, found, err := request.SessionStore.LoadUploadSession(ctx, request.SessionID)
	if err != nil {
		return UploadSessionState{}, err
	}
	if found {
		if err := state.ValidateShape(totalBytes, request.ChunkSize, totalChunks); err != nil {
			return UploadSessionState{}, err
		}
		return state, nil
	}
	return NewUploadSessionState(request.SessionID, totalBytes, request.ChunkSize)
}
