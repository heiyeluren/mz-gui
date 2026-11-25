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

func TestOAuthDeviceFlowStartsDeviceAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/device" {
			t.Fatalf("路径异常: %s", request.URL.Path)
		}
		if request.Method != http.MethodPost {
			t.Fatalf("方法异常: %s", request.Method)
		}
		if got := request.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q", got)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.Form.Get("client_id") != "desktop-client" || request.Form.Get("scope") != "profile offline_access" {
			t.Fatalf("表单异常: %#v", request.Form)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"device_code":"device-code-1",
			"user_code":"ABCD-EFGH",
			"verification_uri":"https://login.example.test/device",
			"verification_uri_complete":"https://login.example.test/device?user_code=ABCD-EFGH",
			"expires_in":600,
			"interval":5
		}`))
	}))
	defer server.Close()
	flow := oauthFlowForServer(t, server.URL)
	response, err := flow.StartDeviceAuthorization(context.Background(), OAuthDeviceAuthorizationRequest{
		DeviceAuthorizationURL: server.URL + "/device",
		ClientID:               "desktop-client",
		Scope:                  "profile offline_access",
		RequestID:              "oauth-start-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.DeviceCode != "device-code-1" || response.UserCode != "ABCD-EFGH" || response.Interval != 5*time.Second || response.ExpiresIn != 10*time.Minute {
		t.Fatalf("设备授权响应异常: %#v", response)
	}
}

func TestOAuthDeviceFlowClassifiesPendingSlowDownAndSuccess(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/token" {
			t.Fatalf("路径异常: %s", request.URL.Path)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.Form.Get("grant_type") != OAuthGrantDeviceCode || request.Form.Get("device_code") != "device-code-1" {
			t.Fatalf("token 表单异常: %#v", request.Form)
		}
		writer.Header().Set("Content-Type", "application/json")
		calls++
		switch calls {
		case 1:
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"authorization_pending"}`))
		case 2:
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"slow_down"}`))
		default:
			_, _ = writer.Write([]byte(`{"access_token":"access-value","refresh_token":"refresh-value","token_type":"Bearer","expires_in":3600,"scope":"profile"}`))
		}
	}))
	defer server.Close()
	flow := oauthFlowForServer(t, server.URL)
	pending, err := flow.PollDeviceToken(context.Background(), OAuthDeviceTokenRequest{
		TokenURL: server.URL + "/token", ClientID: "desktop-client", DeviceCode: "device-code-1", Interval: 5 * time.Second,
	})
	if err != nil || !pending.Pending || pending.NextInterval != 5*time.Second {
		t.Fatalf("pending 分类异常: result=%#v err=%v", pending, err)
	}
	slowDown, err := flow.PollDeviceToken(context.Background(), OAuthDeviceTokenRequest{
		TokenURL: server.URL + "/token", ClientID: "desktop-client", DeviceCode: "device-code-1", Interval: 5 * time.Second,
	})
	if err != nil || !slowDown.Pending || slowDown.NextInterval != 10*time.Second {
		t.Fatalf("slow_down 分类异常: result=%#v err=%v", slowDown, err)
	}
	success, err := flow.PollDeviceToken(context.Background(), OAuthDeviceTokenRequest{
		TokenURL: server.URL + "/token", ClientID: "desktop-client", DeviceCode: "device-code-1", Interval: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if success.Pending || success.Token.AccessToken != "access-value" || success.Token.RefreshToken != "refresh-value" || success.Token.ExpiresIn != time.Hour {
		t.Fatalf("token 成功响应异常: %#v", success)
	}
}

func TestOAuthDeviceFlowRefreshesTokenAndRejectsTerminalErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.Form.Get("refresh_token") {
		case "refresh-value":
			if request.Form.Get("grant_type") != OAuthGrantRefreshToken {
				t.Fatalf("刷新 grant_type 异常: %#v", request.Form)
			}
			_, _ = writer.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","expires_in":7200}`))
		default:
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"access_denied","error_description":"denied"}`))
		}
	}))
	defer server.Close()
	flow := oauthFlowForServer(t, server.URL)
	token, err := flow.RefreshToken(context.Background(), OAuthRefreshTokenRequest{
		TokenURL: server.URL, ClientID: "desktop-client", RefreshToken: "refresh-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "new-access" || token.RefreshToken != "new-refresh" || token.ExpiresIn != 2*time.Hour {
		t.Fatalf("刷新响应异常: %#v", token)
	}
	_, err = flow.RefreshToken(context.Background(), OAuthRefreshTokenRequest{
		TokenURL: server.URL, ClientID: "desktop-client", RefreshToken: "bad-refresh",
	})
	if !errors.Is(err, ErrOAuthAccessDenied) {
		t.Fatalf("终态错误应分类为 access denied: %v", err)
	}
}

func oauthFlowForServer(t *testing.T, rawURL string) *OAuthDeviceFlow {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultHTTPPolicy()
	policy.AllowedHosts = set(parsed.Hostname())
	policy.AllowedMethods[http.MethodPost] = struct{}{}
	client, err := NewClient(ClientOptions{Policy: policy, MaxBodyBytes: 64 << 10})
	if err != nil {
		t.Fatal(err)
	}
	flow, err := NewOAuthDeviceFlow(client)
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

func TestOAuthDeviceFlowRejectsInvalidConfig(t *testing.T) {
	if _, err := NewOAuthDeviceFlow(nil); !errors.Is(err, ErrInvalidOAuthConfig) {
		t.Fatalf("空 client 必须拒绝: %v", err)
	}
	flow := oauthFlowForServer(t, "http://127.0.0.1:1")
	_, err := flow.StartDeviceAuthorization(context.Background(), OAuthDeviceAuthorizationRequest{DeviceAuthorizationURL: "file:///tmp/a", ClientID: "c"})
	if !errors.Is(err, ErrInvalidOAuthConfig) && !strings.Contains(err.Error(), "策略") {
		t.Fatalf("非法授权 URL 必须拒绝: %v", err)
	}
}
