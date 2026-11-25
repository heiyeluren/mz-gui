package remoteservice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	OAuthGrantDeviceCode   = "urn:ietf:params:oauth:grant-type:device_code"
	OAuthGrantRefreshToken = "refresh_token"
)

type OAuthDeviceAuthorizationRequest struct {
	DeviceAuthorizationURL string
	ClientID               string
	Scope                  string
	Audience               string
	RequestID              string
	Timeout                time.Duration
}

type OAuthDeviceAuthorizationResponse struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               time.Duration
	Interval                time.Duration
}

type OAuthDeviceTokenRequest struct {
	TokenURL   string
	ClientID   string
	DeviceCode string
	RequestID  string
	Timeout    time.Duration
	Interval   time.Duration
	ExtraForm  url.Values
}

type OAuthRefreshTokenRequest struct {
	TokenURL     string
	ClientID     string
	RefreshToken string
	Scope        string
	RequestID    string
	Timeout      time.Duration
	ExtraForm    url.Values
}

type OAuthToken struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ExpiresIn    time.Duration
}

type OAuthTokenResult struct {
	Pending      bool
	NextInterval time.Duration
	Token        OAuthToken
	ErrorCode    string
}

type OAuthDeviceFlow struct {
	client *Client
}

func NewOAuthDeviceFlow(client *Client) (*OAuthDeviceFlow, error) {
	if client == nil {
		return nil, ErrInvalidOAuthConfig
	}
	return &OAuthDeviceFlow{client: client}, nil
}

func (flow *OAuthDeviceFlow) StartDeviceAuthorization(ctx context.Context, request OAuthDeviceAuthorizationRequest) (OAuthDeviceAuthorizationResponse, error) {
	if flow == nil || flow.client == nil || ctx == nil || request.ClientID == "" || !validOAuthURL(request.DeviceAuthorizationURL) {
		return OAuthDeviceAuthorizationResponse{}, ErrInvalidOAuthConfig
	}
	form := url.Values{}
	form.Set("client_id", request.ClientID)
	if request.Scope != "" {
		form.Set("scope", request.Scope)
	}
	if request.Audience != "" {
		form.Set("audience", request.Audience)
	}
	response, err := flow.postForm(ctx, request.DeviceAuthorizationURL, form, request.RequestID, "", request.Timeout)
	if err != nil {
		return OAuthDeviceAuthorizationResponse{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return OAuthDeviceAuthorizationResponse{}, oauthError(response)
	}
	var payload oauthDeviceAuthorizationPayload
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return OAuthDeviceAuthorizationResponse{}, err
	}
	if payload.DeviceCode == "" || payload.UserCode == "" || payload.VerificationURI == "" {
		return OAuthDeviceAuthorizationResponse{}, ErrOAuthFailed
	}
	interval := time.Duration(payload.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return OAuthDeviceAuthorizationResponse{
		DeviceCode: payload.DeviceCode, UserCode: payload.UserCode,
		VerificationURI: payload.VerificationURI, VerificationURIComplete: payload.VerificationURIComplete,
		ExpiresIn: time.Duration(payload.ExpiresIn) * time.Second, Interval: interval,
	}, nil
}

func (flow *OAuthDeviceFlow) PollDeviceToken(ctx context.Context, request OAuthDeviceTokenRequest) (OAuthTokenResult, error) {
	if flow == nil || flow.client == nil || ctx == nil || request.ClientID == "" || request.DeviceCode == "" || !validOAuthURL(request.TokenURL) {
		return OAuthTokenResult{}, ErrInvalidOAuthConfig
	}
	form := cloneValues(request.ExtraForm)
	form.Set("grant_type", OAuthGrantDeviceCode)
	form.Set("device_code", request.DeviceCode)
	form.Set("client_id", request.ClientID)
	response, err := flow.postForm(ctx, request.TokenURL, form, request.RequestID, "", request.Timeout)
	if err != nil {
		return OAuthTokenResult{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result, terminalErr := oauthPollingResult(response, request.Interval)
		return result, terminalErr
	}
	token, err := decodeOAuthToken(response.Body)
	if err != nil {
		return OAuthTokenResult{}, err
	}
	return OAuthTokenResult{Token: token}, nil
}

func (flow *OAuthDeviceFlow) RefreshToken(ctx context.Context, request OAuthRefreshTokenRequest) (OAuthToken, error) {
	if flow == nil || flow.client == nil || ctx == nil || request.ClientID == "" || request.RefreshToken == "" || !validOAuthURL(request.TokenURL) {
		return OAuthToken{}, ErrInvalidOAuthConfig
	}
	form := cloneValues(request.ExtraForm)
	form.Set("grant_type", OAuthGrantRefreshToken)
	form.Set("refresh_token", request.RefreshToken)
	form.Set("client_id", request.ClientID)
	if request.Scope != "" {
		form.Set("scope", request.Scope)
	}
	response, err := flow.postForm(ctx, request.TokenURL, form, request.RequestID, "", request.Timeout)
	if err != nil {
		return OAuthToken{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return OAuthToken{}, oauthError(response)
	}
	return decodeOAuthToken(response.Body)
}

func (flow *OAuthDeviceFlow) postForm(ctx context.Context, rawURL string, form url.Values, requestID, idempotencyKey string, timeout time.Duration) (*Response, error) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	return flow.client.Do(ctx, Request{
		Method: http.MethodPost, URL: rawURL, Headers: headers, Body: []byte(form.Encode()),
		RequestID: requestID, IdempotencyKey: idempotencyKey, Timeout: timeout,
	})
}

type oauthDeviceAuthorizationPayload struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

type oauthTokenPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

type oauthErrorPayload struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func decodeOAuthToken(body []byte) (OAuthToken, error) {
	var payload oauthTokenPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return OAuthToken{}, err
	}
	if payload.AccessToken == "" {
		return OAuthToken{}, ErrOAuthFailed
	}
	return OAuthToken{
		AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, TokenType: payload.TokenType,
		Scope: payload.Scope, ExpiresIn: time.Duration(payload.ExpiresIn) * time.Second,
	}, nil
}

func oauthPollingResult(response *Response, interval time.Duration) (OAuthTokenResult, error) {
	payload := decodeOAuthError(response)
	switch payload.Error {
	case "authorization_pending":
		return OAuthTokenResult{Pending: true, NextInterval: normalizedOAuthInterval(interval), ErrorCode: payload.Error}, nil
	case "slow_down":
		return OAuthTokenResult{Pending: true, NextInterval: normalizedOAuthInterval(interval) + 5*time.Second, ErrorCode: payload.Error}, nil
	default:
		return OAuthTokenResult{ErrorCode: payload.Error}, oauthErrorFromPayload(payload, response.StatusCode)
	}
}

func oauthError(response *Response) error {
	return oauthErrorFromPayload(decodeOAuthError(response), response.StatusCode)
}

func oauthErrorFromPayload(payload oauthErrorPayload, status int) error {
	switch payload.Error {
	case "access_denied":
		return ErrOAuthAccessDenied
	case "expired_token":
		return ErrOAuthExpired
	case "":
		return fmt.Errorf("%w: status=%d", ErrOAuthFailed, status)
	default:
		return fmt.Errorf("%w: %s", ErrOAuthFailed, payload.Error)
	}
}

func decodeOAuthError(response *Response) oauthErrorPayload {
	var payload oauthErrorPayload
	if response != nil {
		_ = json.Unmarshal(response.Body, &payload)
	}
	return payload
}

func normalizedOAuthInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 5 * time.Second
	}
	return interval
}

func validOAuthURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func cloneValues(values url.Values) url.Values {
	result := make(url.Values, len(values)+4)
	for key, items := range values {
		result[key] = append([]string(nil), items...)
	}
	return result
}
