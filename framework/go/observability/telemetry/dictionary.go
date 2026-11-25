package telemetry

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const DictionarySchemaVersion = 1

var (
	ErrInvalidDictionary = errors.New("语义事件字典无效")
	ErrUnknownEvent      = errors.New("语义事件未登记")
	ErrEventProperties   = errors.New("语义事件属性不符合字典")
)

//go:embed event-dictionary.schema.json
var dictionarySchema []byte

type PrivacyLevel string

const (
	PrivacyOperational PrivacyLevel = "operational"
	PrivacySensitive   PrivacyLevel = "sensitive"
	PrivacyRestricted  PrivacyLevel = "restricted"
)

// EventDefinition 是产品无关的字典条目，不包含任何应用页面名。
type EventDefinition struct {
	Name               string
	PrivacyLevel       PrivacyLevel
	RequiredProperties []string
	AllowedProperties  []string
}

// Dictionary 是 Framework 字典校验端口：应用注册自己的条目后再校验事件。
type Dictionary struct {
	schemaVersion int
	events        []EventDefinition
	byName        map[string]EventDefinition
}

type dictionaryDocument struct {
	SchemaRef     string                    `json:"$schema"`
	SchemaVersion int                       `json:"schema_version"`
	Events        []dictionaryEventDocument `json:"events"`
}

type dictionaryEventDocument struct {
	Name               string   `json:"name"`
	PrivacyLevel       string   `json:"privacy_level"`
	RequiredProperties []string `json:"required_properties"`
	AllowedProperties  []string `json:"allowed_properties"`
}

func DictionarySchemaJSON() []byte {
	return append([]byte(nil), dictionarySchema...)
}

func ParseDictionary(data []byte) (Dictionary, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Dictionary{}, ErrInvalidDictionary
	}
	var document dictionaryDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Dictionary{}, fmt.Errorf("%w: %v", ErrInvalidDictionary, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Dictionary{}, ErrInvalidDictionary
	}
	if document.SchemaVersion != DictionarySchemaVersion || len(document.Events) == 0 {
		return Dictionary{}, ErrInvalidDictionary
	}
	if document.SchemaRef != "" && strings.TrimSpace(document.SchemaRef) != document.SchemaRef {
		return Dictionary{}, ErrInvalidDictionary
	}
	events := make([]EventDefinition, 0, len(document.Events))
	byName := make(map[string]EventDefinition, len(document.Events))
	for _, item := range document.Events {
		definition, err := parseEventDefinition(item)
		if err != nil {
			return Dictionary{}, err
		}
		if _, exists := byName[definition.Name]; exists {
			return Dictionary{}, fmt.Errorf("%w: 重复事件名 %s", ErrInvalidDictionary, definition.Name)
		}
		byName[definition.Name] = definition
		events = append(events, definition)
	}
	return Dictionary{schemaVersion: document.SchemaVersion, events: events, byName: byName}, nil
}

func (dictionary Dictionary) Lookup(name string) (EventDefinition, bool) {
	definition, exists := dictionary.byName[strings.ToLower(strings.TrimSpace(name))]
	if !exists {
		return EventDefinition{}, false
	}
	return cloneEventDefinition(definition), true
}

func (dictionary Dictionary) Names() []string {
	names := make([]string, 0, len(dictionary.events))
	for _, event := range dictionary.events {
		names = append(names, event.Name)
	}
	sort.Strings(names)
	return names
}

// Apply 按已登记条目校验事件名、必填字段，并只保留允许的属性键。
func (dictionary Dictionary) Apply(event Event) (Event, error) {
	definition, exists := dictionary.Lookup(event.Name)
	if !exists {
		return Event{}, ErrUnknownEvent
	}
	allowed := make(map[string]struct{}, len(definition.AllowedProperties))
	for _, key := range definition.AllowedProperties {
		allowed[key] = struct{}{}
	}
	filtered := make(map[string]string, len(event.Properties))
	for key, value := range event.Properties {
		if _, ok := allowed[key]; !ok || strings.TrimSpace(value) == "" {
			continue
		}
		filtered[key] = value
	}
	for _, key := range definition.RequiredProperties {
		if strings.TrimSpace(filtered[key]) == "" {
			return Event{}, fmt.Errorf("%w: 缺少必填属性 %s", ErrEventProperties, key)
		}
	}
	event.Properties = filtered
	return event, nil
}

func parseEventDefinition(item dictionaryEventDocument) (EventDefinition, error) {
	name := strings.ToLower(strings.TrimSpace(item.Name))
	privacy := PrivacyLevel(strings.ToLower(strings.TrimSpace(item.PrivacyLevel)))
	if !eventNamePattern.MatchString(name) || !validPrivacyLevel(privacy) {
		return EventDefinition{}, fmt.Errorf("%w: 事件 %q 标识无效", ErrInvalidDictionary, item.Name)
	}
	allowed, err := parsePropertyKeys(item.AllowedProperties)
	if err != nil {
		return EventDefinition{}, fmt.Errorf("%w: 事件 %s 允许属性无效", ErrInvalidDictionary, name)
	}
	required, err := parsePropertyKeys(item.RequiredProperties)
	if err != nil {
		return EventDefinition{}, fmt.Errorf("%w: 事件 %s 必填属性无效", ErrInvalidDictionary, name)
	}
	for key := range required {
		if _, exists := allowed[key]; !exists {
			return EventDefinition{}, fmt.Errorf("%w: 事件 %s 必填属性未允许", ErrInvalidDictionary, name)
		}
	}
	definition := EventDefinition{
		Name: name, PrivacyLevel: privacy,
		RequiredProperties: uniqueSortedKeys(item.RequiredProperties),
		AllowedProperties:  uniqueSortedKeys(item.AllowedProperties),
	}
	return definition, nil
}

func parsePropertyKeys(keys []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(keys))
	for _, raw := range keys {
		key := strings.ToLower(strings.TrimSpace(raw))
		if len(key) > MaxPropertyKey || !propertyKeyPattern.MatchString(key) || blockedKeyPattern.MatchString(key) {
			return nil, ErrInvalidDictionary
		}
		if _, exists := result[key]; exists {
			return nil, ErrInvalidDictionary
		}
		result[key] = struct{}{}
	}
	return result, nil
}

func uniqueSortedKeys(keys []string) []string {
	result := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, raw := range keys {
		key := strings.ToLower(strings.TrimSpace(raw))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func validPrivacyLevel(level PrivacyLevel) bool {
	switch level {
	case PrivacyOperational, PrivacySensitive, PrivacyRestricted:
		return true
	default:
		return false
	}
}

func cloneEventDefinition(definition EventDefinition) EventDefinition {
	cloned := definition
	cloned.RequiredProperties = append([]string(nil), definition.RequiredProperties...)
	cloned.AllowedProperties = append([]string(nil), definition.AllowedProperties...)
	return cloned
}
