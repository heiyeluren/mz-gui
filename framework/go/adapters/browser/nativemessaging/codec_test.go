package nativemessaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestCodecReadsFragmentedAndConsecutiveFrames(t *testing.T) {
	first := encodedFrame(t, []byte(`{"type":"host.hello"}`))
	second := encodedFrame(t, []byte(`{"type":"host.heartbeat"}`))
	reader := &chunkReader{data: append(first, second...), chunkSize: 2}
	codec := New(reader, io.Discard, Options{})

	for _, expected := range []string{"host.hello", "host.heartbeat"} {
		var message struct {
			Type string `json:"type"`
		}
		if err := codec.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message.Type != expected {
			t.Fatalf("type = %q, want %q", message.Type, expected)
		}
	}
	var ignored map[string]any
	if err := codec.ReadJSON(&ignored); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF error = %v", err)
	}
}

func TestCodecRejectsInvalidFrames(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		max    uint32
		target error
	}{
		{name: "空消息", input: []byte{0, 0, 0, 0}, target: ErrEmptyFrame},
		{name: "截断头", input: []byte{1, 0}, target: ErrTruncatedFrame},
		{name: "截断正文", input: append([]byte{4, 0, 0, 0}, []byte("{}")...), target: ErrTruncatedFrame},
		{name: "超过应用上限", input: []byte{9, 0, 0, 0}, max: 8, target: ErrFrameTooLarge},
		{name: "非法 JSON", input: encodedFrame(t, []byte(`{"type":`)), target: ErrInvalidJSON},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			codec := New(bytes.NewReader(item.input), io.Discard, Options{MaxMessageBytes: item.max})
			var result map[string]any
			if err := codec.ReadJSON(&result); !errors.Is(err, item.target) {
				t.Fatalf("error = %v, want %v", err, item.target)
			}
		})
	}
}

func TestCodecWritesLittleEndianJSONFrame(t *testing.T) {
	var output bytes.Buffer
	codec := New(bytes.NewReader(nil), &output, Options{})
	if err := codec.WriteJSON(map[string]any{"type": "host.heartbeat", "protocol": 1}); err != nil {
		t.Fatal(err)
	}
	data := output.Bytes()
	if len(data) < 5 {
		t.Fatalf("frame too short: %d", len(data))
	}
	length := binary.LittleEndian.Uint32(data[:4])
	if int(length) != len(data)-4 {
		t.Fatalf("length = %d, payload = %d", length, len(data)-4)
	}
	var decoded map[string]any
	if err := New(bytes.NewReader(data), io.Discard, Options{}).ReadJSON(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["type"] != "host.heartbeat" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestCodecRejectsOversizedOutputAndShortWrites(t *testing.T) {
	codec := New(bytes.NewReader(nil), io.Discard, Options{MaxMessageBytes: 8})
	if err := codec.WriteJSON(map[string]string{"value": "too-large"}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized output error = %v", err)
	}

	codec = New(bytes.NewReader(nil), zeroWriter{}, Options{})
	if err := codec.WriteJSON(map[string]int{"protocol": 1}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write error = %v", err)
	}
}

func encodedFrame(t *testing.T, payload []byte) []byte {
	t.Helper()
	frame := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	return frame
}

type chunkReader struct {
	data      []byte
	chunkSize int
}

func (reader *chunkReader) Read(target []byte) (int, error) {
	if len(reader.data) == 0 {
		return 0, io.EOF
	}
	count := reader.chunkSize
	if count > len(target) {
		count = len(target)
	}
	if count > len(reader.data) {
		count = len(reader.data)
	}
	copy(target, reader.data[:count])
	reader.data = reader.data[count:]
	return count, nil
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) {
	return 0, nil
}
