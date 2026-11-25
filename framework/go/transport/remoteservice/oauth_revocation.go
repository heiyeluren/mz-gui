package remoteservice

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	OAuthTokenTypeAccessToken  = "access_token"
	OAuthTokenTypeRefreshToken = "refresh_token"
)

type OAuthRevocationClientOptions struct {
	Client *Client
}

type OAuthRevocationRequest struct {
	RevocationURL  string
	ClientID       string
	Token          string
	TokenTypeHint  string
	RequestID      string
	IdempotencyKey string
	Timeout        time.Duration
	ExtraForm      url.Values
}

type OAuthRevocationClient struct {
	client *Client
}

func NewOAuthRevocationClient(options OAuthRevocationClientOptions) (*OAuthRevocationClient, error) {
	if options.Client == nil {
		return nil, ErrInvalidOAuthConfig
	}
	return &OAuthRevocationClient{client: options.Client}, nil
}

func (client *OAuthRevocationClient) RevokeToken(ctx context.Context, request OAuthRevocationRequest) error {
	if client == nil || client.client == nil || ctx == nil || !validOAuthRevocationRequest(request) {
		return ErrInvalidOAuthConfig
	}
	form := cloneValues(request.ExtraForm)
	form.Set("token", request.Token)
	form.Set("client_id", request.ClientID)
	if request.TokenTypeHint != "" {
		form.Set("token_type_hint", request.TokenTypeHint)
	}
	headers := http.Header{}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.client.Do(ctx, Request{
		Method: http.MethodPost, URL: request.RevocationURL, Headers: headers, Body: []byte(form.Encode()),
		RequestID: request.RequestID, IdempotencyKey: request.IdempotencyKey, Timeout: request.Timeout,
	})
	if err != nil {
		return err
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	return oauthRevocationError(response)
}

func validOAuthRevocationRequest(request OAuthRevocationRequest) bool {
	if request.ClientID == "" || request.Token == "" || !validOAuthURL(request.RevocationURL) {
		return false
	}
	switch request.TokenTypeHint {
	case "", OAuthTokenTypeAccessToken, OAuthTokenTypeRefreshToken:
		return true
	default:
		return false
	}
}

func oauthRevocationError(response *Response) error {
	payload := decodeOAuthError(response)
	switch payload.Error {
	case "access_denied":
		return ErrOAuthAccessDenied
	case "expired_token":
		return ErrOAuthExpired
	case "":
		if response == nil {
			return ErrOAuthFailed
		}
		return fmt.Errorf("%w: status=%d", ErrOAuthFailed, response.StatusCode)
	default:
		return ErrOAuthFailed
	}
}
