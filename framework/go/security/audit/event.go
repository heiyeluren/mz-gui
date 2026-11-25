package audit

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const RedactedValue = "[REDACTED]"

var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)

type ActorKind string

const (
	ActorKindUser    ActorKind = "user"
	ActorKindProcess ActorKind = "process"
	ActorKindSystem  ActorKind = "system"
)

type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
	DecisionError Decision = "error"
)

type Actor struct {
	Kind ActorKind `json:"kind"`
	ID   string    `json:"id"`
}

func (actor Actor) String() string {
	if actor.Kind == "" || actor.ID == "" {
		return ""
	}
	return string(actor.Kind) + ":" + actor.ID
}

func (actor Actor) Validate() error {
	switch actor.Kind {
	case ActorKindUser, ActorKindProcess, ActorKindSystem:
	default:
		return fmt.Errorf("非法 actor 类型: %s", actor.Kind)
	}
	if !identifierPattern.MatchString(actor.ID) {
		return fmt.Errorf("非法 actor ID: %s", actor.ID)
	}
	return nil
}

type Resource struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
}

func (resource Resource) Validate() error {
	if resource.Type == "" && resource.ID == "" {
		return nil
	}
	if !identifierPattern.MatchString(resource.Type) {
		return fmt.Errorf("非法 resource type: %s", resource.Type)
	}
	if !identifierPattern.MatchString(resource.ID) {
		return fmt.Errorf("非法 resource ID: %s", resource.ID)
	}
	return nil
}

type Event struct {
	ID         string         `json:"id"`
	OccurredAt time.Time      `json:"occurred_at"`
	Actor      Actor          `json:"actor"`
	Action     string         `json:"action"`
	Capability string         `json:"capability,omitempty"`
	Resource   Resource       `json:"resource,omitempty"`
	Decision   Decision       `json:"decision"`
	ReasonCode string         `json:"reason_code,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Record struct {
	AuditID      string
	OccurredAt   string
	Actor        string
	Action       string
	Capability   string
	ResourceType string
	ResourceID   string
	Decision     string
	ReasonCode   string
	RequestID    string
	MetadataJSON string
}

func (event Event) Validate() error {
	if !identifierPattern.MatchString(event.ID) {
		return fmt.Errorf("非法 audit ID: %s", event.ID)
	}
	if event.OccurredAt.IsZero() {
		return fmt.Errorf("审计事件发生时间不能为空")
	}
	if err := event.Actor.Validate(); err != nil {
		return err
	}
	if !identifierPattern.MatchString(event.Action) {
		return fmt.Errorf("非法 action: %s", event.Action)
	}
	if event.Capability != "" && !identifierPattern.MatchString(event.Capability) {
		return fmt.Errorf("非法 capability: %s", event.Capability)
	}
	if err := event.Resource.Validate(); err != nil {
		return err
	}
	switch event.Decision {
	case DecisionAllow, DecisionDeny, DecisionError:
	default:
		return fmt.Errorf("非法 decision: %s", event.Decision)
	}
	if event.ReasonCode != "" && !identifierPattern.MatchString(event.ReasonCode) {
		return fmt.Errorf("非法 reason_code: %s", event.ReasonCode)
	}
	if event.RequestID != "" && !identifierPattern.MatchString(event.RequestID) {
		return fmt.Errorf("非法 request_id: %s", event.RequestID)
	}
	return nil
}

func (event Event) Record() (Record, error) {
	if err := event.Validate(); err != nil {
		return Record{}, err
	}
	metadata := RedactMetadata(event.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return Record{}, fmt.Errorf("序列化审计 metadata: %w", err)
	}
	return Record{
		AuditID: event.ID, OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano),
		Actor: event.Actor.String(), Action: event.Action, Capability: event.Capability,
		ResourceType: event.Resource.Type, ResourceID: event.Resource.ID, Decision: string(event.Decision),
		ReasonCode: event.ReasonCode, RequestID: event.RequestID, MetadataJSON: string(metadataJSON),
	}, nil
}

func RedactMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	redacted := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if isSensitiveKey(key) {
			redacted[key] = RedactedValue
			continue
		}
		redacted[key] = redactValue(value)
	}
	return redacted
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return RedactMetadata(typed)
	case []any:
		values := make([]any, len(typed))
		for index, item := range typed {
			values[index] = redactValue(item)
		}
		return values
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	compact := strings.ReplaceAll(strings.ReplaceAll(normalized, "_", ""), "-", "")
	for _, part := range []string{"token", "secret", "password", "passwd", "authorization", "cookie", "privatekey", "webhook", "apikey", "credential", "path", "url", "deviceid", "serialnumber"} {
		if strings.Contains(normalized, part) || strings.Contains(compact, part) {
			return true
		}
	}
	return false
}
