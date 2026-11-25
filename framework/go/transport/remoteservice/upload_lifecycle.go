package remoteservice

import (
	"context"
	"time"
)

type UploadSessionLifecycleRequest struct {
	Session        UploadSessionState
	RequestID      string
	IdempotencyKey string
	Metadata       map[string]string
}

type UploadSessionLifecycleResult struct {
	StatusCode  int
	CompletedAt time.Time
	AbortedAt   time.Time
}

type UploadSessionLifecycle interface {
	CompleteUploadSession(ctx context.Context, request UploadSessionLifecycleRequest) (UploadSessionLifecycleResult, error)
	AbortUploadSession(ctx context.Context, request UploadSessionLifecycleRequest) (UploadSessionLifecycleResult, error)
}

func buildUploadLifecycleRequest(state UploadSessionState, request UploadFileRequest, suffix string) UploadSessionLifecycleRequest {
	idempotencyKey := request.IdempotencyKey
	if idempotencyKey != "" && suffix != "" {
		idempotencyKey += ":" + suffix
	}
	metadata := make(map[string]string, len(request.LifecycleMetadata))
	for key, value := range request.LifecycleMetadata {
		metadata[key] = value
	}
	return UploadSessionLifecycleRequest{
		Session:        state.Clone(),
		RequestID:      request.RequestID,
		IdempotencyKey: idempotencyKey,
		Metadata:       metadata,
	}
}
