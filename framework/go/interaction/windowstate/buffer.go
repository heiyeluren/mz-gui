package windowstate

import (
	"strings"
	"sync"
)

// Buffer 为已注册页面提供进程内 payload 暂存，供 Save/Restore codec 复用。
type Buffer struct {
	mu    sync.Mutex
	pages map[string][]byte
}

func (buffer *Buffer) Codec(routeID string) PageCodec {
	routeID = strings.TrimSpace(routeID)
	return PageCodec{
		RouteID: routeID,
		Save: func(windowID string) ([]byte, error) {
			return buffer.Get(windowID, routeID), nil
		},
		Restore: func(windowID string, payload []byte) error {
			buffer.Put(windowID, routeID, payload)
			return nil
		},
	}
}

func (buffer *Buffer) Put(windowID, routeID string, payload []byte) {
	if buffer == nil {
		return
	}
	windowID = strings.TrimSpace(windowID)
	routeID = strings.TrimSpace(routeID)
	if windowID == "" || routeID == "" {
		return
	}
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.pages == nil {
		buffer.pages = make(map[string][]byte)
	}
	if len(payload) == 0 {
		buffer.pages[pageKey(windowID, routeID)] = []byte("{}")
		return
	}
	buffer.pages[pageKey(windowID, routeID)] = append([]byte(nil), payload...)
}

func (buffer *Buffer) Get(windowID, routeID string) []byte {
	if buffer == nil {
		return []byte("{}")
	}
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	payload, ok := buffer.pages[pageKey(strings.TrimSpace(windowID), strings.TrimSpace(routeID))]
	if !ok || len(payload) == 0 {
		return []byte("{}")
	}
	return append([]byte(nil), payload...)
}
