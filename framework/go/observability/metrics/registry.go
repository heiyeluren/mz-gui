package metrics

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type Kind string

const (
	Counter   Kind = "counter"
	Gauge     Kind = "gauge"
	Histogram Kind = "histogram"
)

var (
	ErrInvalidDescriptor = errors.New("指标描述符无效")
	ErrMetricExists      = errors.New("指标已存在")
	ErrMetricNotFound    = errors.New("指标不存在")
	metricNamePattern    = regexp.MustCompile(`^mzgui_[a-z][a-z0-9]*(?:_[a-z0-9]+){1,20}$`)
	labelPattern         = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
)

var allowedLabels = map[string]struct{}{
	"process_role":   {},
	"component":      {},
	"result":         {},
	"error_class":    {},
	"resource_group": {},
	"queue":          {},
	"operation":      {},
	"method":         {},
	"status":         {},
	"platform":       {},
}

var blockedLabels = map[string]struct{}{
	"request_id": {},
	"trace_id":   {},
	"span_id":    {},
	"path":       {},
	"url":        {},
	"user_id":    {},
	"account_id": {},
	"session_id": {},
	"file":       {},
}

type Descriptor struct {
	Name        string
	Kind        Kind
	Unit        string
	Description string
	Labels      []string
}

type Registry struct {
	mu      sync.RWMutex
	metrics map[string]Descriptor
}

func NewRegistry() *Registry {
	return &Registry{metrics: make(map[string]Descriptor)}
}

func (registry *Registry) Register(descriptor Descriptor) error {
	normalized, err := Normalize(descriptor)
	if err != nil {
		return err
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.metrics[normalized.Name]; exists {
		return ErrMetricExists
	}
	registry.metrics[normalized.Name] = normalized
	return nil
}

func (registry *Registry) Get(name string) (Descriptor, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	descriptor, exists := registry.metrics[normalized]
	if !exists {
		return Descriptor{}, ErrMetricNotFound
	}
	return descriptor, nil
}

func (registry *Registry) List() []Descriptor {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	names := make([]string, 0, len(registry.metrics))
	for name := range registry.metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Descriptor, 0, len(names))
	for _, name := range names {
		result = append(result, registry.metrics[name])
	}
	return result
}

func Normalize(descriptor Descriptor) (Descriptor, error) {
	descriptor.Name = strings.ToLower(strings.TrimSpace(descriptor.Name))
	descriptor.Unit = strings.ToLower(strings.TrimSpace(descriptor.Unit))
	descriptor.Description = strings.TrimSpace(descriptor.Description)
	if !metricNamePattern.MatchString(descriptor.Name) || !descriptor.Kind.Valid() || descriptor.Unit == "" || descriptor.Description == "" {
		return Descriptor{}, ErrInvalidDescriptor
	}
	labels, err := normalizeLabels(descriptor.Labels)
	if err != nil {
		return Descriptor{}, err
	}
	descriptor.Labels = labels
	return descriptor, nil
}

func (kind Kind) Valid() bool {
	switch kind {
	case Counter, Gauge, Histogram:
		return true
	default:
		return false
	}
}

func normalizeLabels(labels []string) ([]string, error) {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(labels))
	for _, label := range labels {
		normalized := strings.ToLower(strings.TrimSpace(label))
		if !labelPattern.MatchString(normalized) {
			return nil, ErrInvalidDescriptor
		}
		if _, blocked := blockedLabels[normalized]; blocked {
			return nil, ErrInvalidDescriptor
		}
		if _, allowed := allowedLabels[normalized]; !allowed {
			return nil, ErrInvalidDescriptor
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result, nil
}
