package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	securitysession "github.com/heiyeluren/mz-gui/framework/go/security/session"
	bootstrapv1 "github.com/heiyeluren/mz-gui/gen/go/mzgui/framework/bootstrap/v1"
	"google.golang.org/protobuf/proto"
)

func TestHandshakeIssuesBoundSessionAndVerifiableDigest(t *testing.T) {
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	service, store := testService(t, &now)
	request := validRequest(now)
	response, err := service.Handshake(context.Background(), request)
	if err != nil || !response.GetAccepted() {
		t.Fatalf("Framework 握手失败: response=%#v err=%v", response, err)
	}
	if response.GetNegotiatedProtocol().GetMajor() != ProtocolMajor || response.GetNegotiatedProtocol().GetMinor() != ProtocolMinor ||
		response.GetServer().GetProcessRole() != "agentd" || response.GetHealthStatus() != "ready" || len(response.GetServerNonce()) != 32 ||
		len(response.GetSessionToken()) != 64 || len(response.GetHandshakeSha256()) != 64 {
		t.Fatalf("Framework 握手响应不完整: %#v", response)
	}
	if err := VerifyResponse(request, response, serverIdentity(), now); err != nil {
		t.Fatalf("客户端校验合法响应失败: %v", err)
	}
	if _, err := store.ValidateCredential(response.GetSessionId(), string(response.GetSessionToken())); err != nil {
		t.Fatalf("Framework 会话没有进入统一 SessionStore: %v", err)
	}

	tampered := proto.Clone(response).(*bootstrapv1.BootstrapHandshakeResponse)
	tampered.Server.BuildId = "other-build"
	if err := VerifyResponse(request, tampered, serverIdentity(), now); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("被篡改响应必须拒绝: %v", err)
	}

	legacy := validRequest(now)
	legacy.Protocol.Minor = 0
	legacy.ClientNonce = []byte("legacy-minor-zero-nonce-value-000")
	legacyResponse, err := service.Handshake(context.Background(), legacy)
	if err != nil || !legacyResponse.GetAccepted() || legacyResponse.GetNegotiatedProtocol().GetMinor() != 0 {
		t.Fatalf("minor 0 客户端兼容握手失败: response=%#v err=%v", legacyResponse, err)
	}
}

func TestHandshakeProtocolCompatibilityMatrix(t *testing.T) {
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		serverMinor     uint32
		minimumMinor    uint32
		clientMajor     uint32
		clientMinor     uint32
		accepted        bool
		negotiatedMinor uint32
		rejection       string
	}{
		{name: "客户端等于服务端版本", serverMinor: 3, minimumMinor: 1, clientMajor: 1, clientMinor: 3, accepted: true, negotiatedMinor: 3},
		{name: "客户端高于服务端时向下协商", serverMinor: 3, minimumMinor: 1, clientMajor: 1, clientMinor: 7, accepted: true, negotiatedMinor: 3},
		{name: "客户端处于最低兼容版本", serverMinor: 3, minimumMinor: 1, clientMajor: 1, clientMinor: 1, accepted: true, negotiatedMinor: 1},
		{name: "客户端低于最低兼容版本", serverMinor: 3, minimumMinor: 2, clientMajor: 1, clientMinor: 1, rejection: "INCOMPATIBLE_PROTOCOL"},
		{name: "主版本不兼容", serverMinor: 3, minimumMinor: 1, clientMajor: 2, clientMinor: 3, rejection: "INCOMPATIBLE_PROTOCOL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _ := testServiceWithProtocol(t, &now, test.serverMinor, test.minimumMinor)
			request := validRequest(now)
			request.Protocol.Major = test.clientMajor
			request.Protocol.Minor = test.clientMinor
			request.ClientNonce = []byte("protocol-matrix-nonce-value-0001")
			response, err := service.Handshake(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if test.accepted {
				if !response.GetAccepted() || response.GetNegotiatedProtocol().GetMinor() != test.negotiatedMinor ||
					response.GetCompatibility().GetMinimumMinor() != test.minimumMinor || response.GetCompatibility().GetMaximumMinor() != test.serverMinor {
					t.Fatalf("兼容协商错误: %#v", response)
				}
				if err := VerifyResponse(request, response, serverIdentity(), now); err != nil {
					t.Fatalf("协商响应无法通过客户端校验: %v", err)
				}
				return
			}
			if response.GetAccepted() || response.GetRejectionCode() != test.rejection {
				t.Fatalf("不兼容拒绝错误: %#v", response)
			}
		})
	}
}

func TestHandshakeRejectsIdentityCapabilityClockAndReplayFailures(t *testing.T) {
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*bootstrapv1.BootstrapHandshakeRequest)
		code   string
	}{
		{name: "主版本不兼容", mutate: func(value *bootstrapv1.BootstrapHandshakeRequest) { value.Protocol.Major++ }, code: "INCOMPATIBLE_PROTOCOL"},
		{name: "应用不一致", mutate: func(value *bootstrapv1.BootstrapHandshakeRequest) {
			value.Client.Application.AppId = "com.example.other"
		}, code: "INCOMPATIBLE_APPLICATION"},
		{name: "构建不一致", mutate: func(value *bootstrapv1.BootstrapHandshakeRequest) { value.Client.BuildId = "other-build" }, code: "INCOMPATIBLE_BUILD"},
		{name: "Catalog 不一致", mutate: func(value *bootstrapv1.BootstrapHandshakeRequest) {
			value.Client.Application.FrameworkCatalogRevision++
		}, code: "INCOMPATIBLE_APPLICATION"},
		{name: "未知角色", mutate: func(value *bootstrapv1.BootstrapHandshakeRequest) { value.Client.ProcessRole = "unknown" }, code: "UNKNOWN_ROLE"},
		{name: "进程名错配", mutate: func(value *bootstrapv1.BootstrapHandshakeRequest) { value.Client.ProcessName = "mz-gui-helper" }, code: "INVALID_CLIENT"},
		{name: "越权 capability", mutate: func(value *bootstrapv1.BootstrapHandshakeRequest) {
			value.RequestedCapabilities = []string{"helper.execute"}
		}, code: "CAPABILITY_NOT_GRANTED"},
		{name: "短 nonce", mutate: func(value *bootstrapv1.BootstrapHandshakeRequest) { value.ClientNonce = []byte("short") }, code: "INVALID_NONCE"},
		{name: "请求时间过期", mutate: func(value *bootstrapv1.BootstrapHandshakeRequest) {
			value.RequestedAtUnixMs = now.Add(-10 * time.Minute).UnixMilli()
		}, code: "CLOCK_SKEW"},
		{name: "启动晚于请求", mutate: func(value *bootstrapv1.BootstrapHandshakeRequest) {
			value.ClientStartedAtUnixMs = now.Add(time.Second).UnixMilli()
		}, code: "INVALID_CLIENT"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _ := testService(t, &now)
			request := validRequest(now)
			test.mutate(request)
			response, err := service.Handshake(context.Background(), request)
			if err != nil || response.GetAccepted() || response.GetRejectionCode() != test.code {
				t.Fatalf("拒绝结果错误: response=%#v err=%v", response, err)
			}
		})
	}

	service, _ := testService(t, &now)
	request := validRequest(now)
	first, _ := service.Handshake(context.Background(), request)
	second, err := service.Handshake(context.Background(), request)
	if err != nil || !first.GetAccepted() || second.GetAccepted() || second.GetRejectionCode() != "REPLAY_DETECTED" {
		t.Fatalf("nonce 重放未被拒绝: first=%#v second=%#v err=%v", first, second, err)
	}
}

func TestHandshakeConsumesNonceExactlyOnceUnderConcurrency(t *testing.T) {
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	request := validRequest(now)
	var group sync.WaitGroup
	results := make(chan *bootstrapv1.BootstrapHandshakeResponse, 16)
	for index := 0; index < 16; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			response, _ := service.Handshake(context.Background(), proto.Clone(request).(*bootstrapv1.BootstrapHandshakeRequest))
			results <- response
		}()
	}
	group.Wait()
	close(results)
	accepted := 0
	for response := range results {
		if response.GetAccepted() {
			accepted++
		} else if response.GetRejectionCode() != "REPLAY_DETECTED" {
			t.Fatalf("并发拒绝码错误: %#v", response)
		}
	}
	if accepted != 1 {
		t.Fatalf("并发相同 nonce 成功次数=%d，期望 1", accepted)
	}
}

func TestSessionRenewalRotatesCredentialAndRevocationBroadcasts(t *testing.T) {
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	service, store := testService(t, &now)
	subscription, err := store.SubscribeEvents()
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Cancel()
	handshake, err := service.Handshake(context.Background(), validRequest(now))
	if err != nil || !handshake.GetAccepted() {
		t.Fatalf("握手失败: response=%#v err=%v", handshake, err)
	}
	<-subscription.Events()
	now = now.Add(10 * time.Minute)
	renewed, err := service.RenewSession(context.Background(), &bootstrapv1.SessionRenewRequest{
		SessionId: handshake.GetSessionId(), SessionToken: handshake.GetSessionToken(),
	})
	if err != nil || !renewed.GetAccepted() || bytes.Equal(renewed.GetSessionToken(), handshake.GetSessionToken()) ||
		renewed.GetExpiresAtUnixMs() != now.Add(15*time.Minute).UnixMilli() {
		t.Fatalf("续签响应错误: response=%#v err=%v", renewed, err)
	}
	if _, err := store.ValidateCredential(handshake.GetSessionId(), string(handshake.GetSessionToken())); !errors.Is(err, securitysession.ErrInvalidSession) {
		t.Fatalf("续签后旧 token 未失效: %v", err)
	}
	if event := <-subscription.Events(); event.Kind != securitysession.EventRenewed || event.SessionID != handshake.GetSessionId() {
		t.Fatalf("续签事件错误: %#v", event)
	}
	invalid, err := service.RenewSession(context.Background(), &bootstrapv1.SessionRenewRequest{
		SessionId: handshake.GetSessionId(), SessionToken: handshake.GetSessionToken(),
	})
	if err != nil || invalid.GetAccepted() || invalid.GetRejectionCode() != "INVALID_SESSION" {
		t.Fatalf("旧 token 续签未被稳定拒绝: response=%#v err=%v", invalid, err)
	}
	revoked, err := service.RevokeSession(context.Background(), &bootstrapv1.SessionRevokeRequest{
		SessionId: handshake.GetSessionId(), SessionToken: renewed.GetSessionToken(), Reason: securitysession.RevokeReasonClientShutdown,
	})
	if err != nil || !revoked.GetAccepted() {
		t.Fatalf("撤销失败: response=%#v err=%v", revoked, err)
	}
	if event := <-subscription.Events(); event.Kind != securitysession.EventRevoked || event.Reason != securitysession.RevokeReasonClientShutdown {
		t.Fatalf("撤销广播错误: %#v", event)
	}
	if _, err := store.Validate(handshake.GetSessionId()); !errors.Is(err, securitysession.ErrInvalidSession) {
		t.Fatalf("撤销后会话仍有效: %v", err)
	}
}

func TestSessionMutationRPCRejectsMalformedAndStaleCredentials(t *testing.T) {
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	for _, request := range []*bootstrapv1.SessionRenewRequest{
		nil,
		{SessionId: "bad id", SessionToken: bytes.Repeat([]byte{1}, 64)},
		{SessionId: "unknown-session", SessionToken: []byte("short")},
	} {
		response, err := service.RenewSession(context.Background(), request)
		if err != nil || response.GetAccepted() || response.GetRejectionCode() != "INVALID_REQUEST" {
			t.Fatalf("非法续签请求结果错误: response=%#v err=%v", response, err)
		}
	}
	unknown, err := service.RenewSession(context.Background(), &bootstrapv1.SessionRenewRequest{
		SessionId: "unknown-session", SessionToken: bytes.Repeat([]byte{1}, 64),
	})
	if err != nil || unknown.GetAccepted() || unknown.GetRejectionCode() != "INVALID_SESSION" {
		t.Fatalf("未知会话拒绝错误: response=%#v err=%v", unknown, err)
	}
	badReason, err := service.RevokeSession(context.Background(), &bootstrapv1.SessionRevokeRequest{
		SessionId: "unknown-session", SessionToken: bytes.Repeat([]byte{1}, 64), Reason: "policy_changed",
	})
	if err != nil || badReason.GetAccepted() || badReason.GetRejectionCode() != "INVALID_REQUEST" {
		t.Fatalf("客户端越权撤销 reason 未拒绝: response=%#v err=%v", badReason, err)
	}
	stale, err := service.RevokeSession(context.Background(), &bootstrapv1.SessionRevokeRequest{
		SessionId: "unknown-session", SessionToken: bytes.Repeat([]byte{1}, 64), Reason: securitysession.RevokeReasonClientShutdown,
	})
	if err != nil || stale.GetAccepted() || stale.GetRejectionCode() != "INVALID_SESSION" {
		t.Fatalf("未知会话撤销拒绝错误: response=%#v err=%v", stale, err)
	}
}

func TestNewRejectsUnsafeOrIncompleteConfiguration(t *testing.T) {
	now := time.Now().UTC()
	store := newSessionStore(t, &now)
	valid := Config{ProtocolMajor: ProtocolMajor, ProtocolMinor: ProtocolMinor, MinimumMinor: 0, Server: serverIdentity(),
		Roles:    map[string]RolePolicy{"desktop": {ProcessName: "mz-gui-desktop", Capabilities: []string{"system.health"}}},
		Sessions: store, Now: func() time.Time { return now }, Random: bytes.NewReader(bytes.Repeat([]byte{1}, 128)),
		MaxClockSkew: 2 * time.Minute, NonceTTL: 5 * time.Minute, MaxNonces: 64}
	for _, mutate := range []func(*Config){
		func(value *Config) { value.Sessions = nil },
		func(value *Config) { value.Server.Application.FrameworkCatalogSHA256 = "bad" },
		func(value *Config) {
			value.Roles["desktop"] = RolePolicy{ProcessName: "bad/name", Capabilities: []string{"system.health"}}
		},
		func(value *Config) { value.MaxNonces = 0 },
	} {
		candidate := valid
		candidate.Roles = clonePolicies(valid.Roles)
		mutate(&candidate)
		if _, err := New(candidate); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("非法配置必须拒绝，实际为 %v", err)
		}
	}
}

func testService(t *testing.T, now *time.Time) (*Service, *securitysession.SessionStore) {
	t.Helper()
	return testServiceWithProtocol(t, now, ProtocolMinor, 0)
}

func testServiceWithProtocol(t *testing.T, now *time.Time, protocolMinor, minimumMinor uint32) (*Service, *securitysession.SessionStore) {
	t.Helper()
	store := newSessionStore(t, now)
	service, err := New(Config{
		ProtocolMajor: ProtocolMajor, ProtocolMinor: protocolMinor, MinimumMinor: minimumMinor,
		Server: serverIdentity(),
		Roles: map[string]RolePolicy{
			"desktop":      {ProcessName: "mz-gui-desktop", Capabilities: []string{"system.health", "task.read"}},
			"browser-host": {ProcessName: "mz-gui-browser-host", Capabilities: []string{"system.health", "browser.proxy"}},
		},
		Sessions: store, Now: func() time.Time { return *now }, Random: bytes.NewReader(bytes.Repeat([]byte{7}, 4096)),
		MaxClockSkew: 2 * time.Minute, NonceTTL: 5 * time.Minute, MaxNonces: 128,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func newSessionStore(t *testing.T, now *time.Time) *securitysession.SessionStore {
	t.Helper()
	sequence := 0
	tokenSequence := 0
	store, err := securitysession.NewSessionStore(securitysession.StoreOptions{
		TTL: 15 * time.Minute, MaxSessions: 128, Now: func() time.Time { return *now },
		NewID: func() (string, error) {
			sequence++
			return fmt.Sprintf("session-%d", sequence), nil
		},
		NewToken: func() (string, error) {
			tokenSequence++
			return fmt.Sprintf("%064d", tokenSequence), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func serverIdentity() ProcessIdentity {
	return ProcessIdentity{
		ProcessRole: "agentd", ProcessName: "mz-gui-agentd", ProcessVersion: "0.1.0", BuildID: "build-20260815",
		InstanceID: "agentd-instance-1", OS: "darwin", Arch: "arm64", ReleaseChannel: "stable", EndpointID: "mz-gui-agentd",
		Application: ApplicationIdentity{AppID: "com.mz.mzgui", FrameworkAPIVersion: "1.0.0", FrameworkCatalogRevision: 1,
			FrameworkCatalogSHA256:   "c8f166b9741039236c9399e139cb83426f992a003042d83c3f90971ee50faed5",
			AppManifestSHA256:        "16d7b5d2429162649a2e498f04322029d6ed008f70a94774f21fb10d77b024c0",
			FrameworkSelectionSHA256: "1e219237b3f9c4be6c53e4ca0d38f56ad5cc954630043f08e7aa9268897f5895"},
	}
}

func validRequest(now time.Time) *bootstrapv1.BootstrapHandshakeRequest {
	identity := serverIdentity()
	identity.ProcessRole = "desktop"
	identity.ProcessName = "mz-gui-desktop"
	identity.InstanceID = "desktop-instance-1"
	return &bootstrapv1.BootstrapHandshakeRequest{
		Protocol: &bootstrapv1.ProtocolVersion{Major: ProtocolMajor, Minor: ProtocolMinor}, Client: identity.Proto(),
		ClientNonce: []byte("0123456789abcdef0123456789abcdef"), ClientStartedAtUnixMs: now.Add(-time.Minute).UnixMilli(),
		RequestedAtUnixMs: now.UnixMilli(), RequestedCapabilities: []string{"system.health", "task.read"},
	}
}

func clonePolicies(source map[string]RolePolicy) map[string]RolePolicy {
	result := make(map[string]RolePolicy, len(source))
	for key, value := range source {
		value.Capabilities = append([]string(nil), value.Capabilities...)
		result[key] = value
	}
	return result
}
