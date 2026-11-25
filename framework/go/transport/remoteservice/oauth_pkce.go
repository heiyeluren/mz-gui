package remoteservice

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	OAuthGrantAuthorizationCode = "authorization_code"
	OAuthPKCEChallengeS256      = "S256"
)

var oauthPKCEVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var oauthRedirectSchemePattern = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

type OAuthPKCE struct {
	Verifier  string `json:"verifier"`
	Challenge string `json:"challenge"`
	Method    string `json:"method"`
}

func (pkce OAuthPKCE) String() string {
	if pkce.Challenge == "" {
		return "oauth-pkce:<invalid>"
	}
	return "oauth-pkce:" + pkce.Method + ":" + pkce.Challenge
}

type OAuthAuthorizationCodeFlowOptions struct {
	Client *Client
}

type OAuthAuthorizationURLRequest struct {
	AuthorizationURL string
	ClientID         string
	RedirectURI      string
	Scope            string
	State            string
	Audience         string
	PKCE             OAuthPKCE
	ExtraQuery       url.Values
}

type OAuthAuthorizationCodeTokenRequest struct {
	TokenURL    string
	ClientID    string
	Code        string
	RedirectURI string
	PKCE        OAuthPKCE
	RequestID   string
	Timeout     time.Duration
	ExtraForm   url.Values
}

type OAuthAuthorizationCodeFlow struct {
	client *Client
}

func GenerateOAuthPKCE(random io.Reader) (OAuthPKCE, error) {
	if random == nil {
		random = cryptorand.Reader
	}
	seed := make([]byte, 32)
	if _, err := io.ReadFull(random, seed); err != nil {
		return OAuthPKCE{}, err
	}
	return NewOAuthPKCE(base64.RawURLEncoding.EncodeToString(seed))
}

func NewOAuthPKCE(verifier string) (OAuthPKCE, error) {
	if !validOAuthPKCEVerifier(verifier) {
		return OAuthPKCE{}, ErrInvalidOAuthConfig
	}
	return OAuthPKCE{
		Verifier:  verifier,
		Challenge: oauthPKCEChallenge(verifier),
		Method:    OAuthPKCEChallengeS256,
	}, nil
}

func NewOAuthAuthorizationCodeFlow(options OAuthAuthorizationCodeFlowOptions) (*OAuthAuthorizationCodeFlow, error) {
	if options.Client == nil {
		return nil, ErrInvalidOAuthConfig
	}
	return &OAuthAuthorizationCodeFlow{client: options.Client}, nil
}

func (flow *OAuthAuthorizationCodeFlow) BuildAuthorizationURL(request OAuthAuthorizationURLRequest) (string, error) {
	if flow == nil || flow.client == nil || request.ClientID == "" || !validOAuthURL(request.AuthorizationURL) || !validOAuthRedirectURI(request.RedirectURI) {
		return "", ErrInvalidOAuthConfig
	}
	challenge, err := resolveOAuthPKCEChallenge(request.PKCE)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(request.AuthorizationURL)
	if err != nil {
		return "", ErrInvalidOAuthConfig
	}
	query := cloneValues(request.ExtraQuery)
	query.Set("response_type", "code")
	query.Set("client_id", request.ClientID)
	query.Set("redirect_uri", request.RedirectURI)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", OAuthPKCEChallengeS256)
	if request.Scope != "" {
		query.Set("scope", request.Scope)
	}
	if request.State != "" {
		query.Set("state", request.State)
	}
	if request.Audience != "" {
		query.Set("audience", request.Audience)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (flow *OAuthAuthorizationCodeFlow) ExchangeCode(ctx context.Context, request OAuthAuthorizationCodeTokenRequest) (OAuthToken, error) {
	if flow == nil || flow.client == nil || ctx == nil || request.ClientID == "" || request.Code == "" ||
		!validOAuthURL(request.TokenURL) || !validOAuthRedirectURI(request.RedirectURI) || !validOAuthPKCEVerifier(request.PKCE.Verifier) {
		return OAuthToken{}, ErrInvalidOAuthConfig
	}
	form := cloneValues(request.ExtraForm)
	form.Set("grant_type", OAuthGrantAuthorizationCode)
	form.Set("code", request.Code)
	form.Set("redirect_uri", request.RedirectURI)
	form.Set("client_id", request.ClientID)
	form.Set("code_verifier", request.PKCE.Verifier)
	response, err := flow.postForm(ctx, request.TokenURL, form, request.RequestID, "", request.Timeout)
	if err != nil {
		return OAuthToken{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return OAuthToken{}, oauthError(response)
	}
	return decodeOAuthToken(response.Body)
}

func (flow *OAuthAuthorizationCodeFlow) postForm(ctx context.Context, rawURL string, form url.Values, requestID, idempotencyKey string, timeout time.Duration) (*Response, error) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	return flow.client.Do(ctx, Request{
		Method: http.MethodPost, URL: rawURL, Headers: headers, Body: []byte(form.Encode()),
		RequestID: requestID, IdempotencyKey: idempotencyKey, Timeout: timeout,
	})
}

func resolveOAuthPKCEChallenge(pkce OAuthPKCE) (string, error) {
	if !validOAuthPKCEVerifier(pkce.Verifier) {
		return "", ErrInvalidOAuthConfig
	}
	challenge := oauthPKCEChallenge(pkce.Verifier)
	if pkce.Challenge != "" && pkce.Challenge != challenge {
		return "", ErrInvalidOAuthConfig
	}
	if pkce.Method != "" && pkce.Method != OAuthPKCEChallengeS256 {
		return "", ErrInvalidOAuthConfig
	}
	return challenge, nil
}

func oauthPKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func validOAuthPKCEVerifier(verifier string) bool {
	return oauthPKCEVerifierPattern.MatchString(verifier)
}

func validOAuthRedirectURI(rawURI string) bool {
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.User != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "http" || scheme == "https" {
		return parsed.Host != ""
	}
	switch scheme {
	case "javascript", "data", "file", "about", "blob":
		return false
	default:
		return oauthRedirectSchemePattern.MatchString(scheme)
	}
}
