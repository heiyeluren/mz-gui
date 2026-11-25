package remoteservice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOAuthTokenRotatorRefreshesAndStoresTokenBehindSameReference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/token" {
			t.Fatalf("路径异常: %s", request.URL.Path)
		}
		if request.Header.Get("X-Request-ID") != "rotate-1" {
			t.Fatalf("X-Request-ID = %q", request.Header.Get("X-Request-ID"))
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		expected := map[string]string{
			"grant_type":    OAuthGrantRefreshToken,
			"refresh_token": "opaque-refresh-old",
			"client_id":     "desktop-client",
			"scope":         "profile offline_access",
			"audience":      "api://default",
		}
		for key, want := range expected {
			if got := request.Form.Get(key); got != want {
				t.Fatalf("刷新参数 %s = %q, want %q, form=%#v", key, got, want, request.Form)
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"opaque-access-new","refresh_token":"opaque-refresh-new","token_type":"Bearer","expires_in":7200,"scope":"profile"}`))
	}))
	defer server.Close()

	store := newRecordingOAuthCredentialStore()
	vault, err := NewOAuthTokenVault(OAuthTokenVaultOptions{Store: store, Namespace: "oauth"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := vault.Save(context.Background(), OAuthTokenCredentialRef{Provider: "example", Account: "default"}, OAuthToken{
		AccessToken: "opaque-access-old", RefreshToken: "opaque-refresh-old", TokenType: "Bearer", Scope: "profile", ExpiresIn: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	rotator, err := NewOAuthTokenRotator(OAuthTokenRotatorOptions{Vault: vault, Flow: oauthFlowForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := rotator.Rotate(context.Background(), OAuthTokenRotationRequest{
		CredentialRef: ref,
		TokenURL:      server.URL + "/token",
		ClientID:      "desktop-client",
		Scope:         "profile offline_access",
		RequestID:     "rotate-1",
		Timeout:       time.Second,
		ExtraForm:     url.Values{"audience": []string{"api://default"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CredentialRef != ref || result.TokenType != "Bearer" || result.Scope != "profile" || result.ExpiresIn != 2*time.Hour || !result.RefreshTokenRotated {
		t.Fatalf("轮换结果异常: %#v", result)
	}
	loaded, err := vault.Load(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccessToken != "opaque-access-new" || loaded.RefreshToken != "opaque-refresh-new" || loaded.ExpiresIn != 2*time.Hour {
		t.Fatalf("轮换后凭据异常: %#v", loaded)
	}
	if strings.Contains(result.String(), "opaque-access-new") || strings.Contains(result.String(), "opaque-refresh-new") {
		t.Fatalf("轮换结果 String 不应泄漏 token: %s", result.String())
	}
}

func TestOAuthTokenRotatorPreservesRefreshTokenWhenProviderOmitsNewOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"opaque-access-next","token_type":"Bearer","expires_in":3600,"scope":"profile"}`))
	}))
	defer server.Close()

	store := newRecordingOAuthCredentialStore()
	vault, err := NewOAuthTokenVault(OAuthTokenVaultOptions{Store: store, Namespace: "oauth"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := vault.Save(context.Background(), OAuthTokenCredentialRef{Provider: "example", Account: "default"}, OAuthToken{
		AccessToken: "opaque-access-current", RefreshToken: "opaque-refresh-current", TokenType: "Bearer",
	})
	if err != nil {
		t.Fatal(err)
	}
	rotator, err := NewOAuthTokenRotator(OAuthTokenRotatorOptions{Vault: vault, Flow: oauthFlowForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := rotator.Rotate(context.Background(), OAuthTokenRotationRequest{
		CredentialRef: ref,
		TokenURL:      server.URL,
		ClientID:      "desktop-client",
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := vault.Load(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RefreshToken != "opaque-refresh-current" || result.RefreshTokenRotated {
		t.Fatalf("provider 未返回新 refresh token 时必须保留旧值: token=%#v result=%#v", loaded, result)
	}
}

func TestOAuthTokenRotatorRejectsMissingRefreshTokenWithoutNetworkCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()

	store := newRecordingOAuthCredentialStore()
	vault, err := NewOAuthTokenVault(OAuthTokenVaultOptions{Store: store, Namespace: "oauth"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := vault.Save(context.Background(), OAuthTokenCredentialRef{Provider: "example", Account: "default"}, OAuthToken{
		AccessToken: "opaque-access-only", TokenType: "Bearer",
	})
	if err != nil {
		t.Fatal(err)
	}
	rotator, err := NewOAuthTokenRotator(OAuthTokenRotatorOptions{Vault: vault, Flow: oauthFlowForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rotator.Rotate(context.Background(), OAuthTokenRotationRequest{
		CredentialRef: ref,
		TokenURL:      server.URL,
		ClientID:      "desktop-client",
	})
	if !errors.Is(err, ErrOAuthRefreshUnavailable) {
		t.Fatalf("缺少 refresh token 必须拒绝: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("缺少 refresh token 时不应发起网络请求，实际 %d", calls.Load())
	}
	if err != nil && strings.Contains(err.Error(), "opaque-access-only") {
		t.Fatalf("错误不应泄漏 token: %v", err)
	}
}
