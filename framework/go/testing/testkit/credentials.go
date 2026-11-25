package testkit

import (
	"sync"

	"github.com/heiyeluren/mz-gui/framework/go/platform/credentials"
)

type MemoryCredentialBackend struct {
	mu     sync.Mutex
	values map[string]string
}

func NewMemoryCredentialBackend() *MemoryCredentialBackend {
	return &MemoryCredentialBackend{values: make(map[string]string)}
}

func (backend *MemoryCredentialBackend) Get(service string, key string) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	value, ok := backend.values[service+"\x00"+key]
	if !ok {
		return "", credentials.ErrNotFound
	}
	return value, nil
}

func (backend *MemoryCredentialBackend) Set(service string, key string, value string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.values[service+"\x00"+key] = value
	return nil
}

func (backend *MemoryCredentialBackend) Delete(service string, key string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	delete(backend.values, service+"\x00"+key)
	return nil
}
