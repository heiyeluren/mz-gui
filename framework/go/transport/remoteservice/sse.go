package remoteservice

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"time"
)

type SSEOptions struct {
	MaxLineBytes int
	MaxDataBytes int
}

func ParseSSE(ctx context.Context, reader io.Reader, options SSEOptions) ([]SSEEvent, error) {
	if ctx == nil || reader == nil {
		return nil, ErrInvalidRequest
	}
	if options.MaxLineBytes < 1 {
		options.MaxLineBytes = 64 << 10
	}
	if options.MaxDataBytes < 1 {
		options.MaxDataBytes = 1 << 20
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4096), options.MaxLineBytes)
	events := make([]SSEEvent, 0)
	current := SSEEvent{}
	data := bytes.Buffer{}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := scanner.Text()
		if line == "" {
			if data.Len() > 0 || current.Event != "" || current.ID != "" {
				current.Data = strings.TrimSuffix(data.String(), "\n")
				events = append(events, current)
			}
			current = SSEEvent{}
			data.Reset()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			name, value = line, ""
		}
		value = strings.TrimPrefix(value, " ")
		switch name {
		case "event":
			current.Event = value
		case "id":
			current.ID = value
		case "retry":
			milliseconds, err := strconv.Atoi(value)
			if err != nil || milliseconds < 0 {
				return nil, ErrInvalidSSEEvent
			}
			current.Retry = time.Duration(milliseconds) * time.Millisecond
		case "data":
			if data.Len()+len(value)+1 > options.MaxDataBytes {
				return nil, ErrBodyTooLarge
			}
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if data.Len() > 0 || current.Event != "" || current.ID != "" {
		current.Data = strings.TrimSuffix(data.String(), "\n")
		events = append(events, current)
	}
	return events, nil
}
