package terminal

import (
	"sync"
	"time"
	"unicode/utf8"
)

type OutputFrame struct {
	Sequence   uint64
	Data       []byte
	OccurredAt time.Time
}

type OutputSnapshot struct {
	Frames       []OutputFrame
	OldestCursor uint64
	NextCursor   uint64
	Truncated    bool
	HasMore      bool
}

// 4MiB 输出可由大量短 ANSI 帧组成；额外允许约 4MiB 帧元数据，仍拒绝无限小帧增长。
const maxOutputFrames = 65536
const maxSnapshotFrames = 4096

type outputRing struct {
	mu            sync.RWMutex
	maxBytes      int
	usedBytes     int
	sequence      uint64
	partialOldest bool
	frames        []OutputFrame
}

func newOutputRing(maxBytes int) *outputRing {
	if maxBytes < 1 {
		maxBytes = 1
	}
	return &outputRing{maxBytes: maxBytes}
}

func (ring *outputRing) append(data []byte, occurredAt time.Time) {
	if len(data) == 0 {
		return
	}
	ring.mu.Lock()
	defer ring.mu.Unlock()

	ring.sequence++
	ring.partialOldest = false
	if len(data) > ring.maxBytes {
		data = data[len(data)-ring.maxBytes:]
		for len(data) > 0 && !utf8.RuneStart(data[0]) {
			data = data[1:]
		}
		clear(ring.frames)
		ring.frames = ring.frames[:0]
		ring.usedBytes = 0
		ring.partialOldest = true
	}
	for (ring.usedBytes+len(data) > ring.maxBytes || len(ring.frames) >= maxOutputFrames) && len(ring.frames) > 0 {
		ring.usedBytes -= len(ring.frames[0].Data)
		ring.frames[0] = OutputFrame{}
		ring.frames = ring.frames[1:]
	}
	copyData := append([]byte(nil), data...)
	ring.frames = append(ring.frames, OutputFrame{Sequence: ring.sequence, Data: copyData, OccurredAt: occurredAt})
	ring.usedBytes += len(copyData)
}

func (ring *outputRing) snapshot(cursor uint64, maxBytes int) OutputSnapshot {
	if maxBytes < 1 {
		maxBytes = 1
	}
	ring.mu.RLock()
	defer ring.mu.RUnlock()

	result := OutputSnapshot{NextCursor: cursor}
	if len(ring.frames) == 0 {
		return result
	}
	result.OldestCursor = ring.frames[0].Sequence
	result.Truncated = cursor+1 < result.OldestCursor || (ring.partialOldest && cursor < result.OldestCursor)
	used := 0
	for index, frame := range ring.frames {
		if frame.Sequence <= cursor {
			continue
		}
		if len(result.Frames) >= maxSnapshotFrames || (used > 0 && used+len(frame.Data) > maxBytes) {
			result.HasMore = true
			break
		}
		result.Frames = append(result.Frames, OutputFrame{
			Sequence: frame.Sequence, Data: append([]byte(nil), frame.Data...), OccurredAt: frame.OccurredAt,
		})
		used += len(frame.Data)
		result.NextCursor = frame.Sequence
		if index < len(ring.frames)-1 && used >= maxBytes {
			result.HasMore = true
			break
		}
	}
	return result
}
