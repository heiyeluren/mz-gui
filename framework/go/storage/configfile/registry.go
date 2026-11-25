package configfile

import (
	"errors"
	"fmt"
	"regexp"
	"sync"
)

var (
	ErrInvalidSchema  = errors.New("配置 schema 无效")
	ErrSchemaNotFound = errors.New("配置 schema 不存在")
)

var schemaIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)

type Validator func([]byte) error

type Schema struct {
	ID       string
	Version  int
	Validate Validator
}

type Registry struct {
	mu      sync.RWMutex
	schemas map[string]Schema
}

func NewRegistry() *Registry {
	return &Registry{schemas: make(map[string]Schema)}
}

func (registry *Registry) Register(schema Schema) error {
	if registry == nil || !schemaIDPattern.MatchString(schema.ID) || schema.Version < 1 || schema.Validate == nil {
		return ErrInvalidSchema
	}
	key := schemaKey(schema.ID, schema.Version)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.schemas[key]; exists {
		return ErrInvalidSchema
	}
	registry.schemas[key] = schema
	return nil
}

func (registry *Registry) Validate(schemaID string, version int, data []byte) error {
	if registry == nil || !schemaIDPattern.MatchString(schemaID) || version < 1 {
		return ErrInvalidSchema
	}
	registry.mu.RLock()
	schema, exists := registry.schemas[schemaKey(schemaID, version)]
	registry.mu.RUnlock()
	if !exists {
		return ErrSchemaNotFound
	}
	return schema.Validate(cloneBytes(data))
}

func schemaKey(id string, version int) string {
	return fmt.Sprintf("%s@%d", id, version)
}
