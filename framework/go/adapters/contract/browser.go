package contract

import "context"

type PairRequest struct {
	Meta      RequestMeta `json:"meta"`
	BrowserID string      `json:"browser_id"`
}

type PairSession struct {
	ID        modelID `json:"id"`
	BrowserID string  `json:"browser_id"`
}

type SiteAuthorizationRequest struct {
	Meta   RequestMeta `json:"meta"`
	Origin string      `json:"origin"`
	Scopes []string    `json:"scopes"`
}

type SiteGrant struct {
	ID     GrantID  `json:"id"`
	Origin string   `json:"origin"`
	Scopes []string `json:"scopes"`
}

type BrowserOpenRequest struct {
	Meta   RequestMeta `json:"meta"`
	Origin string      `json:"origin"`
	URL    string      `json:"url"`
}

type BrowserContext struct {
	ID modelID `json:"id"`
}

type BrowserAction struct {
	Meta        RequestMeta `json:"meta"`
	ContextID   modelID     `json:"context_id"`
	ActionID    string      `json:"action_id"`
	Parameters  []NameValue `json:"parameters,omitempty"`
	RequiresAck bool        `json:"requires_ack"`
}

type BrowserObserveRequest struct {
	Meta      RequestMeta `json:"meta"`
	ContextID modelID     `json:"context_id"`
	Cursor    string      `json:"cursor,omitempty"`
}

type BrowserEvent struct {
	Sequence uint64 `json:"sequence"`
	Type     string `json:"type"`
	Cursor   string `json:"cursor"`
	Terminal bool   `json:"terminal"`
}

type BrowserEventStream interface {
	Next(context.Context) (BrowserEvent, error)
	Close(context.Context) error
}
