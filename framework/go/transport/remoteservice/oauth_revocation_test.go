package remoteservice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthRevocationClientRevokesTokenWithHintAndRequestID(t *testing.T) {
	var requestID string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/revoke" {
			t.Fatalf("路径异常: %s", request.URL.Path)
		}
		if request.Method != http.MethodPost {
			t.Fatalf("方法异常: %s", request.Method)
		}
		requestID = request.Header.Get("X-Request-ID")
		if contentType := request.Header.Get("Content-Type"); contentType != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q", contentType)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		expected := map[string]string{
			"token":           "opaque-refresh-value",
			"token_type_hint": OAuthTokenTypeRefreshToken,
			"client_id":       "desktop-client",
			"audience":        "api://default",
		}
		for key, want := range expected {
			if got := request.Form.Get(key); got != want {
				t.Fatalf("撤销参数 %s = %q, want %q, form=%#v", key, got, want, request.Form)
			}
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewOAuthRevocationClient(OAuthRevocationClientOptions{Client: oauthClientForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	err = client.RevokeToken(context.Background(), OAuthRevocationRequest{
		RevocationURL: server.URL + "/revoke",
		ClientID:      "desktop-client",
		Token:         "opaque-refresh-value",
		TokenTypeHint: OAuthTokenTypeRefreshToken,
		RequestID:     "revoke-1",
		Timeout:       time.Second,
		ExtraForm:     url.Values{"audience": []string{"api://default"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if requestID != "revoke-1" {
		t.Fatalf("X-Request-ID = %q, want revoke-1", requestID)
	}
}

func TestOAuthRevocationClientTreatsRFC7009SuccessStatusAsRevoked(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusNoContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(status)
			}))
			defer server.Close()

			client, err := NewOAuthRevocationClient(OAuthRevocationClientOptions{Client: oauthClientForServer(t, server.URL)})
			if err != nil {
				t.Fatal(err)
			}
			err = client.RevokeToken(context.Background(), OAuthRevocationRequest{
				RevocationURL: server.URL,
				ClientID:      "desktop-client",
				Token:         "opaque-access-value",
				TokenTypeHint: OAuthTokenTypeAccessToken,
			})
			if err != nil {
				t.Fatalf("status %d 应视为撤销成功: %v", status, err)
			}
		})
	}
}

func TestOAuthRevocationClientRejectsInvalidConfigAndRedactsToken(t *testing.T) {
	if _, err := NewOAuthRevocationClient(OAuthRevocationClientOptions{}); !errors.Is(err, ErrInvalidOAuthConfig) {
		t.Fatalf("nil client 必须拒绝: %v", err)
	}

	client, err := NewOAuthRevocationClient(OAuthRevocationClientOptions{Client: oauthClientForHost(t, "login.example.test")})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []OAuthRevocationRequest{
		{RevocationURL: "https://login.example.test/revoke", ClientID: "desktop-client", Token: ""},
		{RevocationURL: "file:///tmp/revoke", ClientID: "desktop-client", Token: "opaque-value"},
		{RevocationURL: "https://login.example.test/revoke", ClientID: "", Token: "opaque-value"},
		{RevocationURL: "https://login.example.test/revoke", ClientID: "desktop-client", Token: "opaque-value", TokenTypeHint: "unknown"},
	} {
		err := client.RevokeToken(context.Background(), request)
		if !errors.Is(err, ErrInvalidOAuthConfig) {
			t.Fatalf("非法撤销配置必须拒绝: request=%#v err=%v", request, err)
		}
		if err != nil && strings.Contains(err.Error(), request.Token) && request.Token != "" {
			t.Fatalf("错误信息不应泄漏 token: %v", err)
		}
	}
}
