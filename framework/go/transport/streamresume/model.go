// Package streamresume 提供产品无关的本机流式消息恢复状态机。
package streamresume

import (
	"errors"
	"io"
	"regexp"
	"time"
)

var (
	ErrInvalidStream     = errors.New("流恢复参数无效")
	ErrSequenceGap       = errors.New("流消息 sequence 不连续")
	ErrReplayUnavailable = errors.New("流消息重放窗口不可用")
	ErrStreamClosed      = errors.New("流已关闭")
	ErrInvalidToken      = errors.New("流恢复 token 无效")

	streamIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type Frame struct {
	StreamID   string    `json:"stream_id"`
	Sequence   uint64    `json:"sequence"`
	Payload    []byte    `json:"payload"`
	Final      bool      `json:"final"`
	ObservedAt time.Time `json:"observed_at"`
}

type Cursor struct {
	StreamID    string `json:"stream_id"`
	LastAck     uint64 `json:"last_ack"`
	ResumeToken string `json:"resume_token"`
}

type Snapshot struct {
	StreamID      string    `json:"stream_id"`
	NextSequence  uint64    `json:"next_sequence"`
	FirstSequence uint64    `json:"first_sequence"`
	LastSequence  uint64    `json:"last_sequence"`
	Closed        bool      `json:"closed"`
	FrameCount    int       `json:"frame_count"`
	ObservedAt    time.Time `json:"observed_at"`
}

type Options struct {
	MaxFramesPerStream int
	Now                func() time.Time
	Random             io.Reader
}

type AppendRequest struct {
	StreamID string
	Sequence uint64
	Payload  []byte
	Final    bool
}

type ReplayRequest struct {
	Cursor Cursor
	Limit  int
}

type Store struct {
	maxFrames int
	now       func() time.Time
	random    io.Reader
	streams   map[string]*streamState
}

type streamState struct {
	id        string
	token     string
	next      uint64
	closed    bool
	frames    []Frame
	updatedAt time.Time
}

func defaultNow() time.Time {
	return time.Now().UTC()
}
