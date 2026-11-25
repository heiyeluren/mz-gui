package nativemessaging

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

const DefaultMaxMessageBytes uint32 = 256 << 10

var (
	ErrEmptyFrame     = errors.New("Native Messaging 消息为空")
	ErrFrameTooLarge  = errors.New("Native Messaging 消息超过上限")
	ErrTruncatedFrame = errors.New("Native Messaging 消息被截断")
	ErrInvalidJSON    = errors.New("Native Messaging JSON 无效")
)

type Options struct {
	MaxMessageBytes uint32
}

type Codec struct {
	reader  io.Reader
	writer  io.Writer
	max     uint32
	writeMu sync.Mutex
}

func New(reader io.Reader, writer io.Writer, options Options) *Codec {
	maximum := options.MaxMessageBytes
	if maximum == 0 {
		maximum = DefaultMaxMessageBytes
	}
	return &Codec{reader: reader, writer: writer, max: maximum}
}

func (codec *Codec) ReadJSON(target any) error {
	var header [4]byte
	if _, err := io.ReadFull(codec.reader, header[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return io.EOF
		}
		return ErrTruncatedFrame
	}
	length := binary.LittleEndian.Uint32(header[:])
	if length == 0 {
		return ErrEmptyFrame
	}
	if length > codec.max {
		return ErrFrameTooLarge
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(codec.reader, payload); err != nil {
		return ErrTruncatedFrame
	}
	if !json.Valid(payload) || json.Unmarshal(payload, target) != nil {
		return ErrInvalidJSON
	}
	return nil
}

func (codec *Codec) WriteJSON(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return ErrInvalidJSON
	}
	if len(payload) == 0 {
		return ErrEmptyFrame
	}
	if uint64(len(payload)) > uint64(codec.max) {
		return ErrFrameTooLarge
	}
	frame := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)

	codec.writeMu.Lock()
	defer codec.writeMu.Unlock()
	for len(frame) > 0 {
		written, writeErr := codec.writer.Write(frame)
		if writeErr != nil {
			return writeErr
		}
		if written <= 0 || written > len(frame) {
			return io.ErrShortWrite
		}
		frame = frame[written:]
	}
	return nil
}
