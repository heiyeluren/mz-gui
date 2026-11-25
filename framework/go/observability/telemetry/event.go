package telemetry

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	MaxProperties    = 32
	MaxPropertyKey   = 80
	MaxPropertyValue = 256
	MaxBatchSize     = 50
)

var (
	ErrInvalidEvent  = errors.New("语义行为事件无效")
	eventIDPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,127}$`)
	eventNamePattern = regexp.MustCompile(
		`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+){1,15}$`,
	)
	propertyKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+){0,15}$`)
	blockedKeyPattern  = regexp.MustCompile(`(?i)(authorization|token|secret|password|passwd|cookie|jwt|webhook|refresh[_-]?token|app[_-]?secret|client[_-]?secret|open[_-]?id|chat[_-]?id|prompt|input|content|message|chat[_-]?history|raw[_-]?text|path|url)`)
)

// Event 是产品无关的本地语义行为事件，不允许承载正文、路径或凭据。
type Event struct {
	ID         string
	Name       string
	Source     string
	SessionID  string
	Properties map[string]string
	OccurredAt time.Time
}

func Normalize(event Event, now time.Time) (Event, error) {
	event.ID = strings.TrimSpace(event.ID)
	event.Name = strings.ToLower(strings.TrimSpace(event.Name))
	event.Source = strings.ToLower(strings.TrimSpace(event.Source))
	event.SessionID = strings.TrimSpace(event.SessionID)
	if now.IsZero() || !eventIDPattern.MatchString(event.ID) || !eventNamePattern.MatchString(event.Name) ||
		!propertyKeyPattern.MatchString(event.Source) || len(event.SessionID) > 128 || event.OccurredAt.IsZero() ||
		event.OccurredAt.Before(now.Add(-24*time.Hour)) || event.OccurredAt.After(now.Add(5*time.Minute)) {
		return Event{}, ErrInvalidEvent
	}
	event.OccurredAt = event.OccurredAt.UTC()
	event.Properties = sanitizeProperties(event.Properties)
	return event, nil
}

func sanitizeProperties(properties map[string]string) map[string]string {
	if len(properties) == 0 {
		return map[string]string{}
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string, min(len(keys), MaxProperties))
	for _, rawKey := range keys {
		if len(result) >= MaxProperties {
			break
		}
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if len(key) > MaxPropertyKey || !propertyKeyPattern.MatchString(key) || blockedKeyPattern.MatchString(key) {
			continue
		}
		value := strings.TrimSpace(properties[rawKey])
		if value == "" {
			continue
		}
		if len(value) > MaxPropertyValue {
			value = value[:MaxPropertyValue]
		}
		result[key] = value
	}
	return result
}
