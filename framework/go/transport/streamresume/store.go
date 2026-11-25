package streamresume

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"sync"
)

type SafeStore struct {
	mu    sync.RWMutex
	store *Store
}

func New(options Options) (*SafeStore, error) {
	if options.MaxFramesPerStream < 1 || options.MaxFramesPerStream > 65536 {
		return nil, ErrInvalidStream
	}
	now := options.Now
	if now == nil {
		now = defaultNow
	}
	random := options.Random
	if random == nil {
		random = randReader{}
	}
	return &SafeStore{store: &Store{
		maxFrames: options.MaxFramesPerStream, now: now, random: random, streams: make(map[string]*streamState),
	}}, nil
}

func (safe *SafeStore) Open(ctx context.Context, streamID string) (Cursor, error) {
	if safe == nil || ctx == nil || ctx.Err() != nil || !streamIDPattern.MatchString(streamID) {
		return Cursor{}, ErrInvalidStream
	}
	safe.mu.Lock()
	defer safe.mu.Unlock()
	if current, exists := safe.store.streams[streamID]; exists {
		return Cursor{StreamID: streamID, ResumeToken: current.token}, nil
	}
	token, err := safe.store.newToken()
	if err != nil {
		return Cursor{}, err
	}
	safe.store.streams[streamID] = &streamState{id: streamID, token: token, next: 1, updatedAt: safe.store.now().UTC()}
	return Cursor{StreamID: streamID, ResumeToken: token}, nil
}

func (safe *SafeStore) Append(ctx context.Context, request AppendRequest) (Frame, error) {
	if safe == nil || ctx == nil || ctx.Err() != nil || !streamIDPattern.MatchString(request.StreamID) || request.Sequence == 0 {
		return Frame{}, ErrInvalidStream
	}
	safe.mu.Lock()
	defer safe.mu.Unlock()
	stream, exists := safe.store.streams[request.StreamID]
	if !exists {
		return Frame{}, ErrInvalidStream
	}
	if stream.closed {
		return Frame{}, ErrStreamClosed
	}
	if request.Sequence != stream.next {
		return Frame{}, ErrSequenceGap
	}
	frame := Frame{
		StreamID: request.StreamID, Sequence: request.Sequence, Payload: append([]byte(nil), request.Payload...),
		Final: request.Final, ObservedAt: safe.store.now().UTC(),
	}
	stream.frames = append(stream.frames, frame)
	stream.next++
	stream.updatedAt = frame.ObservedAt
	if request.Final {
		stream.closed = true
	}
	if len(stream.frames) > safe.store.maxFrames {
		copy(stream.frames, stream.frames[len(stream.frames)-safe.store.maxFrames:])
		stream.frames = stream.frames[:safe.store.maxFrames]
	}
	return cloneFrame(frame), nil
}

func (safe *SafeStore) Replay(ctx context.Context, request ReplayRequest) ([]Frame, Cursor, error) {
	if safe == nil || ctx == nil || ctx.Err() != nil || request.Limit < 1 || request.Limit > 65536 {
		return nil, Cursor{}, ErrInvalidStream
	}
	safe.mu.RLock()
	defer safe.mu.RUnlock()
	stream, err := safe.store.requireStream(request.Cursor)
	if err != nil {
		return nil, Cursor{}, err
	}
	if len(stream.frames) == 0 {
		return nil, request.Cursor, nil
	}
	first := stream.frames[0].Sequence
	if request.Cursor.LastAck+1 < first {
		return nil, Cursor{}, ErrReplayUnavailable
	}
	result := make([]Frame, 0, min(request.Limit, len(stream.frames)))
	cursor := request.Cursor
	for _, frame := range stream.frames {
		if frame.Sequence <= request.Cursor.LastAck {
			continue
		}
		result = append(result, cloneFrame(frame))
		cursor.LastAck = frame.Sequence
		if len(result) == request.Limit {
			break
		}
	}
	return result, cursor, nil
}

func (safe *SafeStore) Ack(ctx context.Context, cursor Cursor) (Cursor, error) {
	if safe == nil || ctx == nil || ctx.Err() != nil {
		return Cursor{}, ErrInvalidStream
	}
	safe.mu.RLock()
	defer safe.mu.RUnlock()
	stream, err := safe.store.requireStream(cursor)
	if err != nil {
		return Cursor{}, err
	}
	last := stream.next - 1
	if cursor.LastAck > last {
		return Cursor{}, ErrSequenceGap
	}
	return cursor, nil
}

func (safe *SafeStore) Snapshot(streamID string) (Snapshot, error) {
	if safe == nil || !streamIDPattern.MatchString(streamID) {
		return Snapshot{}, ErrInvalidStream
	}
	safe.mu.RLock()
	defer safe.mu.RUnlock()
	stream, exists := safe.store.streams[streamID]
	if !exists {
		return Snapshot{}, ErrInvalidStream
	}
	snapshot := Snapshot{
		StreamID: stream.id, NextSequence: stream.next, Closed: stream.closed,
		FrameCount: len(stream.frames), ObservedAt: stream.updatedAt,
	}
	if len(stream.frames) > 0 {
		snapshot.FirstSequence = stream.frames[0].Sequence
		snapshot.LastSequence = stream.frames[len(stream.frames)-1].Sequence
	}
	return snapshot, nil
}

func (store *Store) requireStream(cursor Cursor) (*streamState, error) {
	if !streamIDPattern.MatchString(cursor.StreamID) || cursor.ResumeToken == "" {
		return nil, ErrInvalidStream
	}
	stream, exists := store.streams[cursor.StreamID]
	if !exists {
		return nil, ErrInvalidStream
	}
	if stream.token != cursor.ResumeToken {
		return nil, ErrInvalidToken
	}
	return stream, nil
}

func (store *Store) newToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := io.ReadFull(store.random, buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func cloneFrame(frame Frame) Frame {
	frame.Payload = append([]byte(nil), frame.Payload...)
	return frame
}

type randReader struct{}

func (randReader) Read(buffer []byte) (int, error) {
	return rand.Read(buffer)
}
