package remoteservice

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
)

type WebSocketOptions struct {
	Policy       Policy
	HTTPClient   *http.Client
	Subprotocols []string
}

func DialWebSocket(ctx context.Context, rawURL string, headers http.Header, options WebSocketOptions) (*websocket.Conn, *http.Response, error) {
	if ctx == nil {
		return nil, nil, ErrInvalidRequest
	}
	policy := options.Policy
	if policy.AllowedSchemes == nil && policy.AllowedMethods == nil && policy.AllowedHeaders == nil {
		policy = DefaultWebSocketPolicy()
	}
	if _, err := policy.Validate(http.MethodGet, rawURL, headers, 0); err != nil {
		return nil, nil, err
	}
	return websocket.Dial(ctx, rawURL, &websocket.DialOptions{
		HTTPClient:   options.HTTPClient,
		HTTPHeader:   headers,
		Subprotocols: options.Subprotocols,
	})
}
