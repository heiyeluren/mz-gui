package contract

import "context"

type AuthRequest struct {
	Meta     RequestMeta `json:"meta"`
	Provider string      `json:"provider"`
	Scopes   []string    `json:"scopes"`
}

type AuthState struct {
	Ref       AuthRef  `json:"ref"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	Scopes    []string `json:"scopes"`
}

type RemoteRequest struct {
	Meta      RequestMeta `json:"meta"`
	ServiceID string      `json:"service_id"`
	Operation string      `json:"operation"`
	Headers   []NameValue `json:"headers,omitempty"`
	BodyRef   ArtifactRef `json:"body_ref,omitempty"`
	AuthRef   AuthRef     `json:"auth_ref,omitempty"`
}

type RemoteResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    []NameValue `json:"headers,omitempty"`
	BodyRef    ArtifactRef `json:"body_ref,omitempty"`
}

type RemoteStreamRequest struct {
	Meta      RequestMeta `json:"meta"`
	ServiceID string      `json:"service_id"`
	Operation string      `json:"operation"`
	Cursor    string      `json:"cursor,omitempty"`
	AuthRef   AuthRef     `json:"auth_ref,omitempty"`
}

type RemoteEvent struct {
	Sequence uint64      `json:"sequence"`
	Type     string      `json:"type"`
	BodyRef  ArtifactRef `json:"body_ref,omitempty"`
	Terminal bool        `json:"terminal"`
}

type RemoteEventStream interface {
	Next(context.Context) (RemoteEvent, error)
	Close(context.Context) error
}
