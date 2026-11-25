package remoteservice

import (
	"context"
	"sync"
	"time"
)

type UploadCompletedChunk struct {
	Index       int
	Start       int64
	End         int64
	Total       int64
	CompletedAt time.Time
}

func (chunk UploadCompletedChunk) Size() int64 {
	if chunk.End < chunk.Start {
		return 0
	}
	return chunk.End - chunk.Start + 1
}

type UploadSessionState struct {
	SessionID       string
	TotalBytes      int64
	ChunkSize       int64
	TotalChunks     int
	CompletedChunks map[int]UploadCompletedChunk
	UpdatedAt       time.Time
}

func NewUploadSessionState(sessionID string, totalBytes, chunkSize int64) (UploadSessionState, error) {
	if sessionID == "" || totalBytes <= 0 || chunkSize <= 0 {
		return UploadSessionState{}, ErrInvalidUpload
	}
	chunks, err := PlanUploadChunks(totalBytes, chunkSize)
	if err != nil {
		return UploadSessionState{}, err
	}
	now := time.Now()
	return UploadSessionState{
		SessionID:       sessionID,
		TotalBytes:      totalBytes,
		ChunkSize:       chunkSize,
		TotalChunks:     len(chunks),
		CompletedChunks: map[int]UploadCompletedChunk{},
		UpdatedAt:       now,
	}, nil
}

func (state UploadSessionState) ValidateShape(totalBytes, chunkSize int64, totalChunks int) error {
	if state.SessionID == "" || state.TotalBytes != totalBytes || state.ChunkSize != chunkSize || state.TotalChunks != totalChunks {
		return ErrUploadSessionConflict
	}
	return nil
}

func (state UploadSessionState) IsChunkCompleted(index int) bool {
	if state.CompletedChunks == nil {
		return false
	}
	_, ok := state.CompletedChunks[index]
	return ok
}

func (state UploadSessionState) CompletedBytes() int64 {
	var total int64
	for _, chunk := range state.CompletedChunks {
		total += chunk.Size()
	}
	return total
}

func (state UploadSessionState) IsComplete() bool {
	return state.TotalChunks > 0 && len(state.CompletedChunks) >= state.TotalChunks
}

func (state *UploadSessionState) MarkChunkCompleted(chunk UploadChunk, completedAt time.Time) {
	if state.CompletedChunks == nil {
		state.CompletedChunks = map[int]UploadCompletedChunk{}
	}
	state.CompletedChunks[chunk.Index] = UploadCompletedChunk{
		Index:       chunk.Index,
		Start:       chunk.Start,
		End:         chunk.End,
		Total:       chunk.Total,
		CompletedAt: completedAt,
	}
	state.UpdatedAt = completedAt
}

func (state UploadSessionState) Clone() UploadSessionState {
	cloned := state
	cloned.CompletedChunks = make(map[int]UploadCompletedChunk, len(state.CompletedChunks))
	for index, chunk := range state.CompletedChunks {
		cloned.CompletedChunks[index] = chunk
	}
	return cloned
}

type UploadSessionStore interface {
	LoadUploadSession(ctx context.Context, sessionID string) (UploadSessionState, bool, error)
	SaveUploadSession(ctx context.Context, state UploadSessionState) error
}

type MemoryUploadSessionStore struct {
	mu       sync.Mutex
	sessions map[string]UploadSessionState
}

func NewMemoryUploadSessionStore() *MemoryUploadSessionStore {
	return &MemoryUploadSessionStore{sessions: map[string]UploadSessionState{}}
}

func (store *MemoryUploadSessionStore) LoadUploadSession(ctx context.Context, sessionID string) (UploadSessionState, bool, error) {
	if store == nil || ctx == nil || sessionID == "" {
		return UploadSessionState{}, false, ErrInvalidUpload
	}
	if err := ctx.Err(); err != nil {
		return UploadSessionState{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	state, ok := store.sessions[sessionID]
	if !ok {
		return UploadSessionState{}, false, nil
	}
	return state.Clone(), true, nil
}

func (store *MemoryUploadSessionStore) SaveUploadSession(ctx context.Context, state UploadSessionState) error {
	if store == nil || ctx == nil || state.SessionID == "" || state.TotalBytes <= 0 || state.ChunkSize <= 0 || state.TotalChunks <= 0 {
		return ErrInvalidUpload
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.sessions == nil {
		store.sessions = map[string]UploadSessionState{}
	}
	store.sessions[state.SessionID] = state.Clone()
	return nil
}
