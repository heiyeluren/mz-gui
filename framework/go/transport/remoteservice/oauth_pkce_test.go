package remoteservice

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGenerateOAuthPKCECreatesURLSafeVerifier(t *testing.T) {
	pkce, err := GenerateOAuthPKCE(bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if len(pkce.Verifier) != 43 || strings.Contains(pkce.Verifier, "=") || pkce.Method != OAuthPKCEChallengeS256 {
		t.Fatalf("生成的 PKCE verifier 异常: %#v", pkce)
	}
	if pkce.Challenge == "" || pkce.Challenge == pkce.Verifier {
		t.Fatalf("生成的 PKCE challenge 异常: %#v", pkce)
	}
}

func TestOAuthPKCEChallengeMatchesRFC7636Vector(t *testing.T) {
	pkce, err := NewOAuthPKCE("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if err != nil {
		t.Fatal(err)
	}
	if pkce.Method != OAuthPKCEChallengeS256 || pkce.Challenge != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("PKCE challenge 不符合 RFC 7636: %#v", pkce)
	}
	if strings.Contains(pkce.String(), pkce.Verifier) {
		t.Fatalf("PKCE String 不应泄漏 verifier: %s", pkce.String())
	}
}

func TestOAuthAuthorizationCodeFlowBuildsAuthorizationURL(t *testing.T) {
	flow, err := NewOAuthAuthorizationCodeFlow(OAuthAuthorizationCodeFlowOptions{Client: oauthClientForHost(t, "login.example.test")})
	if err != nil {
		t.Fatal(err)
	}
	pkce, err := NewOAuthPKCE("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if err != nil {
		t.Fatal(err)
	}
	rawURL, err := flow.BuildAuthorizationURL(OAuthAuthorizationURLRequest{
		AuthorizationURL: "https://login.example.test/oauth/authorize",
		ClientID:         "desktop-client",
		RedirectURI:      "http://127.0.0.1:49152/callback",
		Scope:            "profile offline_access",
		State:            "state-123",
		Audience:         "api://default",
		PKCE:             pkce,
		ExtraQuery:       url.Values{"prompt": []string{"consent"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Scheme != "https" || parsed.Host != "login.example.test" || parsed.Path != "/oauth/authorize" {
		t.Fatalf("授权 URL 基础部分异常: %s", rawURL)
	}
	expected := map[string]string{
		"response_type":         "code",
		"client_id":             "desktop-client",
		"redirect_uri":          "http://127.0.0.1:49152/callback",
		"scope":                 "profile offline_access",
		"state":                 "state-123",
		"audience":              "api://default",
		"code_challenge":        "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		"code_challenge_method": OAuthPKCEChallengeS256,
		"prompt":                "consent",
	}
	for key, want := range expected {
		if got := query.Get(key); got != want {
			t.Fatalf("授权参数 %s = %q, want %q, url=%s", key, got, want, rawURL)
		}
	}
	if strings.Contains(rawURL, pkce.Verifier) {
		t.Fatalf("授权 URL 不得包含 code verifier: %s", rawURL)
	}
}

func TestOAuthAuthorizationCodeFlowExchangesCodeForToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/token" {
			t.Fatalf("路径异常: %s", request.URL.Path)
		}
		if request.Method != http.MethodPost {
			t.Fatalf("方法异常: %s", request.Method)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		expected := map[string]string{
			"grant_type":    OAuthGrantAuthorizationCode,
			"code":          "auth-code-1",
			"redirect_uri":  "http://127.0.0.1:49152/callback",
			"client_id":     "desktop-client",
			"code_verifier": "verifier-abcdefghijklmnopqrstuvwxyz1234567890_ABC",
		}
		for key, want := range expected {
			if got := request.Form.Get(key); got != want {
				t.Fatalf("token 参数 %s = %q, want %q, form=%#v", key, got, want, request.Form)
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"pkce-access","refresh_token":"pkce-refresh","token_type":"Bearer","expires_in":3600,"scope":"profile"}`))
	}))
	defer server.Close()
	flow, err := NewOAuthAuthorizationCodeFlow(OAuthAuthorizationCodeFlowOptions{Client: oauthClientForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	token, err := flow.ExchangeCode(context.Background(), OAuthAuthorizationCodeTokenRequest{
		TokenURL:    server.URL + "/token",
		ClientID:    "desktop-client",
		Code:        "auth-code-1",
		RedirectURI: "http://127.0.0.1:49152/callback",
		PKCE: OAuthPKCE{
			Verifier:  "verifier-abcdefghijklmnopqrstuvwxyz1234567890_ABC",
			Challenge: "challenge",
			Method:    OAuthPKCEChallengeS256,
		},
		RequestID: "pkce-token-1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "pkce-access" || token.RefreshToken != "pkce-refresh" || token.ExpiresIn != time.Hour {
		t.Fatalf("授权码 token 响应异常: %#v", token)
	}
}

func TestOAuthAuthorizationCodeFlowRejectsInvalidPKCEConfig(t *testing.T) {
	if _, err := NewOAuthPKCE("short"); !errors.Is(err, ErrInvalidOAuthConfig) {
		t.Fatalf("短 verifier 必须拒绝: %v", err)
	}
	flow, err := NewOAuthAuthorizationCodeFlow(OAuthAuthorizationCodeFlowOptions{Client: oauthClientForHost(t, "login.example.test")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = flow.BuildAuthorizationURL(OAuthAuthorizationURLRequest{
		AuthorizationURL: "https://login.example.test/oauth/authorize",
		ClientID:         "desktop-client",
		RedirectURI:      "http://127.0.0.1:49152/callback",
		PKCE:             OAuthPKCE{Verifier: "short", Challenge: "bad", Method: OAuthPKCEChallengeS256},
	})
	if !errors.Is(err, ErrInvalidOAuthConfig) {
		t.Fatalf("非法 PKCE 必须拒绝: %v", err)
	}
	pkce, err := NewOAuthPKCE("verifier-abcdefghijklmnopqrstuvwxyz1234567890_ABC")
	if err != nil {
		t.Fatal(err)
	}
	_, err = flow.BuildAuthorizationURL(OAuthAuthorizationURLRequest{
		AuthorizationURL: "https://login.example.test/oauth/authorize",
		ClientID:         "desktop-client",
		RedirectURI:      "javascript:alert(1)",
		PKCE:             pkce,
	})
	if !errors.Is(err, ErrInvalidOAuthConfig) {
		t.Fatalf("危险 redirect URI scheme 必须拒绝: %v", err)
	}
}

func oauthClientForServer(t *testing.T, rawURL string) *Client {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return oauthClientForHost(t, parsed.Hostname())
}

func oauthClientForHost(t *testing.T, host string) *Client {
	t.Helper()
	policy := DefaultHTTPPolicy()
	policy.AllowedHosts = set(host)
	policy.AllowedMethods[http.MethodPost] = struct{}{}
	client, err := NewClient(ClientOptions{Policy: policy, MaxBodyBytes: 64 << 10})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
