package remoteservice

import (
	"net/http"
	"net/url"
	"strings"
)

const defaultMaxBodyBytes int64 = 8 << 20

func DefaultHTTPPolicy() Policy {
	return Policy{
		AllowedSchemes: set("http", "https"),
		AllowedMethods: set(http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete),
		AllowedHeaders: set("accept", "authorization", "content-range", "content-type", "idempotency-key", "if-range", "range", "traceparent", "tracestate", "user-agent", "x-request-id"),
		MaxBodyBytes:   defaultMaxBodyBytes,
	}
}

func DefaultWebSocketPolicy() Policy {
	return Policy{
		AllowedSchemes: set("ws", "wss"),
		AllowedMethods: set(http.MethodGet),
		AllowedHeaders: set("authorization", "sec-websocket-protocol", "user-agent", "x-request-id"),
		MaxBodyBytes:   defaultMaxBodyBytes,
	}
}

func (policy Policy) Validate(method, rawURL string, headers http.Header, bodyBytes int64) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil {
		return nil, ErrInvalidRequest
	}
	scheme := strings.ToLower(parsed.Scheme)
	if len(policy.AllowedSchemes) > 0 {
		if _, ok := policy.AllowedSchemes[scheme]; !ok {
			return nil, ErrPolicyDenied
		}
	}
	host := strings.ToLower(parsed.Hostname())
	if len(policy.AllowedHosts) > 0 {
		if _, ok := policy.AllowedHosts[host]; !ok {
			return nil, ErrPolicyDenied
		}
	}
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	if normalizedMethod == "" || strings.ContainsAny(normalizedMethod, "\r\n\t ") {
		return nil, ErrInvalidRequest
	}
	if len(policy.AllowedMethods) > 0 {
		if _, ok := policy.AllowedMethods[strings.ToLower(normalizedMethod)]; !ok {
			return nil, ErrPolicyDenied
		}
	}
	if policy.MaxBodyBytes < 1 {
		policy.MaxBodyBytes = defaultMaxBodyBytes
	}
	if bodyBytes > policy.MaxBodyBytes {
		return nil, ErrBodyTooLarge
	}
	for name, values := range headers {
		normalizedName := strings.ToLower(strings.TrimSpace(name))
		if normalizedName == "" || strings.ContainsAny(normalizedName, "\r\n:") {
			return nil, ErrInvalidRequest
		}
		if len(policy.AllowedHeaders) > 0 {
			if _, ok := policy.AllowedHeaders[normalizedName]; !ok {
				return nil, ErrPolicyDenied
			}
		}
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n") {
				return nil, ErrInvalidRequest
			}
		}
	}
	return parsed, nil
}

func set(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	return result
}
