package bootstrap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"sync"
	"time"

	securitysession "github.com/heiyeluren/mz-gui/framework/go/security/session"
	bootstrapv1 "github.com/heiyeluren/mz-gui/gen/go/mzgui/framework/bootstrap/v1"
	"google.golang.org/protobuf/proto"
)

const (
	ProtocolMajor uint32 = 1
	ProtocolMinor uint32 = 1
)

var (
	ErrInvalidConfig   = errors.New("Framework Bootstrap 配置无效")
	ErrInvalidResponse = errors.New("Framework Bootstrap 响应无效")

	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	processPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
	capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z][a-z0-9-]*)+$`)
	semverPattern     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)
)

type ApplicationIdentity struct {
	AppID                    string
	FrameworkAPIVersion      string
	FrameworkCatalogRevision uint64
	FrameworkCatalogSHA256   string
	AppManifestSHA256        string
	FrameworkSelectionSHA256 string
}

func (identity ApplicationIdentity) Proto() *bootstrapv1.ApplicationIdentity {
	return &bootstrapv1.ApplicationIdentity{
		AppId: identity.AppID, FrameworkApiVersion: identity.FrameworkAPIVersion,
		FrameworkCatalogRevision: identity.FrameworkCatalogRevision, FrameworkCatalogSha256: identity.FrameworkCatalogSHA256,
		AppManifestSha256: identity.AppManifestSHA256, FrameworkSelectionSha256: identity.FrameworkSelectionSHA256,
	}
}

type ProcessIdentity struct {
	ProcessRole    string
	ProcessName    string
	ProcessVersion string
	BuildID        string
	InstanceID     string
	OS             string
	Arch           string
	ReleaseChannel string
	EndpointID     string
	Application    ApplicationIdentity
}

func (identity ProcessIdentity) Proto() *bootstrapv1.ProcessIdentity {
	return &bootstrapv1.ProcessIdentity{
		ProcessRole: identity.ProcessRole, ProcessName: identity.ProcessName, ProcessVersion: identity.ProcessVersion,
		BuildId: identity.BuildID, InstanceId: identity.InstanceID, Os: identity.OS, Arch: identity.Arch,
		ReleaseChannel: identity.ReleaseChannel, EndpointId: identity.EndpointID, Application: identity.Application.Proto(),
	}
}

type RolePolicy struct {
	ProcessName  string
	Capabilities []string
}

type Config struct {
	ProtocolMajor uint32
	ProtocolMinor uint32
	MinimumMinor  uint32
	Server        ProcessIdentity
	Roles         map[string]RolePolicy
	Sessions      *securitysession.SessionStore
	Now           func() time.Time
	Random        io.Reader
	MaxClockSkew  time.Duration
	NonceTTL      time.Duration
	MaxNonces     int
	HealthStatus  string
	Degraded      []string
}

type Service struct {
	bootstrapv1.UnimplementedBootstrapServiceServer
	config Config
	roles  map[string]rolePolicy

	nonceMu sync.Mutex
	nonces  map[string]time.Time
}

type rolePolicy struct {
	processName  string
	capabilities map[string]struct{}
}

func New(config Config) (*Service, error) {
	if config.ProtocolMajor == 0 || config.MinimumMinor > config.ProtocolMinor || config.Sessions == nil ||
		config.MaxClockSkew < time.Second || config.MaxClockSkew > 10*time.Minute ||
		config.NonceTTL < config.MaxClockSkew || config.NonceTTL > time.Hour || config.MaxNonces < 1 || config.MaxNonces > 65536 {
		return nil, ErrInvalidConfig
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.HealthStatus == "" {
		config.HealthStatus = "ready"
	}
	if config.HealthStatus != "ready" && config.HealthStatus != "degraded" {
		return nil, ErrInvalidConfig
	}
	if config.HealthStatus == "ready" && len(config.Degraded) != 0 || config.HealthStatus == "degraded" && len(config.Degraded) == 0 {
		return nil, ErrInvalidConfig
	}
	if err := validateIdentity(config.Server, true); err != nil || config.Server.ProcessRole != "agentd" {
		return nil, ErrInvalidConfig
	}
	if len(config.Roles) == 0 {
		return nil, ErrInvalidConfig
	}
	roles := make(map[string]rolePolicy, len(config.Roles))
	for role, policy := range config.Roles {
		if !processPattern.MatchString(role) || !processPattern.MatchString(policy.ProcessName) || len(policy.Capabilities) == 0 || len(policy.Capabilities) > securitysession.MaxCapabilities {
			return nil, ErrInvalidConfig
		}
		capabilities := make(map[string]struct{}, len(policy.Capabilities))
		for _, capability := range policy.Capabilities {
			if !capabilityPattern.MatchString(capability) {
				return nil, ErrInvalidConfig
			}
			if _, exists := capabilities[capability]; exists {
				return nil, ErrInvalidConfig
			}
			capabilities[capability] = struct{}{}
		}
		roles[role] = rolePolicy{processName: policy.ProcessName, capabilities: capabilities}
	}
	config.Roles = nil
	config.Degraded = append([]string(nil), config.Degraded...)
	return &Service{config: config, roles: roles, nonces: make(map[string]time.Time)}, nil
}

// NewInstanceID 为进程生命周期生成不可预测实例标识。
func NewInstanceID(role string) (string, error) {
	if !processPattern.MatchString(role) {
		return "", ErrInvalidConfig
	}
	buffer := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return "", fmt.Errorf("生成进程实例标识: %w", err)
	}
	return role + "-" + hex.EncodeToString(buffer), nil
}

func (service *Service) Handshake(_ context.Context, request *bootstrapv1.BootstrapHandshakeRequest) (*bootstrapv1.BootstrapHandshakeResponse, error) {
	now := service.config.Now().UTC()
	if request == nil || request.GetProtocol() == nil || request.GetClient() == nil {
		return rejected("INVALID_REQUEST"), nil
	}
	if request.GetProtocol().GetMajor() != service.config.ProtocolMajor || request.GetProtocol().GetMinor() < service.config.MinimumMinor {
		return rejected("INCOMPATIBLE_PROTOCOL"), nil
	}
	client, err := identityFromProto(request.GetClient())
	if err != nil {
		return rejected("INVALID_CLIENT"), nil
	}
	policy, exists := service.roles[client.ProcessRole]
	if !exists {
		return rejected("UNKNOWN_ROLE"), nil
	}
	if client.ProcessName != policy.processName {
		return rejected("INVALID_CLIENT"), nil
	}
	if !sameApplication(client.Application, service.config.Server.Application) || client.EndpointID != service.config.Server.EndpointID ||
		client.ReleaseChannel != service.config.Server.ReleaseChannel || client.OS != service.config.Server.OS || client.Arch != service.config.Server.Arch {
		return rejected("INCOMPATIBLE_APPLICATION"), nil
	}
	if client.BuildID != service.config.Server.BuildID || client.ProcessVersion != service.config.Server.ProcessVersion {
		return rejected("INCOMPATIBLE_BUILD"), nil
	}
	requestedAt := time.UnixMilli(request.GetRequestedAtUnixMs()).UTC()
	startedAt := time.UnixMilli(request.GetClientStartedAtUnixMs()).UTC()
	if request.GetRequestedAtUnixMs() <= 0 || requestedAt.Before(now.Add(-service.config.MaxClockSkew)) || requestedAt.After(now.Add(service.config.MaxClockSkew)) {
		return rejected("CLOCK_SKEW"), nil
	}
	if request.GetClientStartedAtUnixMs() <= 0 || startedAt.After(requestedAt) {
		return rejected("INVALID_CLIENT"), nil
	}
	if len(request.GetClientNonce()) < 16 || len(request.GetClientNonce()) > 64 {
		return rejected("INVALID_NONCE"), nil
	}
	granted, ok := grantCapabilities(request.GetRequestedCapabilities(), policy.capabilities)
	if !ok {
		return rejected("CAPABILITY_NOT_GRANTED"), nil
	}
	if !service.consumeNonce(client, request.GetClientNonce(), now) {
		return rejected("REPLAY_DETECTED"), nil
	}
	session, err := service.config.Sessions.Issue(client.ProcessRole, client.ProcessVersion, granted)
	if err != nil {
		if errors.Is(err, securitysession.ErrResourceExhausted) {
			return rejected("RESOURCE_EXHAUSTED"), nil
		}
		return rejected("INTERNAL"), nil
	}
	serverNonce := make([]byte, 32)
	if _, err := io.ReadFull(service.config.Random, serverNonce); err != nil {
		service.config.Sessions.Revoke(session.ID)
		return rejected("INTERNAL"), nil
	}
	minor := request.GetProtocol().GetMinor()
	if minor > service.config.ProtocolMinor {
		minor = service.config.ProtocolMinor
	}
	response := &bootstrapv1.BootstrapHandshakeResponse{
		Accepted: true, NegotiatedProtocol: &bootstrapv1.ProtocolVersion{Major: service.config.ProtocolMajor, Minor: minor},
		ServerNonce: serverNonce, SessionId: session.ID, SessionToken: []byte(session.Token), ExpiresAtUnixMs: session.ExpiresAt.UnixMilli(),
		Server: service.config.Server.Proto(), Compatibility: &bootstrapv1.CompatibilityRange{
			Major: service.config.ProtocolMajor, MinimumMinor: service.config.MinimumMinor, MaximumMinor: service.config.ProtocolMinor,
		},
		GrantedCapabilities: granted, HealthStatus: service.config.HealthStatus, DegradedReasons: append([]string(nil), service.config.Degraded...),
	}
	response.HandshakeSha256, err = ComputeHandshakeDigest(request, response)
	if err != nil {
		service.config.Sessions.Revoke(session.ID)
		return rejected("INTERNAL"), nil
	}
	return response, nil
}

// RenewSession 使用当前会话凭据轮换 token，避免长期复用握手凭据。
func (service *Service) RenewSession(ctx context.Context, request *bootstrapv1.SessionRenewRequest) (*bootstrapv1.SessionRenewResponse, error) {
	if ctx == nil || ctx.Err() != nil || request == nil || !identifierPattern.MatchString(request.GetSessionId()) ||
		len(request.GetSessionToken()) < 32 || len(request.GetSessionToken()) > 256 {
		return rejectedRenewal("INVALID_REQUEST"), nil
	}
	session, err := service.config.Sessions.RenewCredential(request.GetSessionId(), string(request.GetSessionToken()))
	if err != nil {
		return rejectedRenewal("INVALID_SESSION"), nil
	}
	return &bootstrapv1.SessionRenewResponse{Accepted: true, SessionToken: []byte(session.Token), ExpiresAtUnixMs: session.ExpiresAt.UnixMilli()}, nil
}

// RevokeSession 只允许持有当前 token 的调用方主动撤销自己的会话。
func (service *Service) RevokeSession(ctx context.Context, request *bootstrapv1.SessionRevokeRequest) (*bootstrapv1.SessionRevokeResponse, error) {
	if ctx == nil || ctx.Err() != nil || request == nil || !identifierPattern.MatchString(request.GetSessionId()) ||
		len(request.GetSessionToken()) < 32 || len(request.GetSessionToken()) > 256 || !validClientRevokeReason(request.GetReason()) {
		return rejectedRevocation("INVALID_REQUEST"), nil
	}
	if !service.config.Sessions.RevokeCredential(request.GetSessionId(), string(request.GetSessionToken()), request.GetReason()) {
		return rejectedRevocation("INVALID_SESSION"), nil
	}
	return &bootstrapv1.SessionRevokeResponse{Accepted: true}, nil
}

func rejectedRenewal(code string) *bootstrapv1.SessionRenewResponse {
	return &bootstrapv1.SessionRenewResponse{Accepted: false, RejectionCode: code}
}

func rejectedRevocation(code string) *bootstrapv1.SessionRevokeResponse {
	return &bootstrapv1.SessionRevokeResponse{Accepted: false, RejectionCode: code}
}

func validClientRevokeReason(reason string) bool {
	return reason == securitysession.RevokeReasonClientShutdown || reason == securitysession.RevokeReasonReplaced
}

func ComputeHandshakeDigest(request *bootstrapv1.BootstrapHandshakeRequest, response *bootstrapv1.BootstrapHandshakeResponse) (string, error) {
	if request == nil || response == nil {
		return "", ErrInvalidResponse
	}
	proof := proto.Clone(response).(*bootstrapv1.BootstrapHandshakeResponse)
	proof.HandshakeSha256 = ""
	requestBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil {
		return "", ErrInvalidResponse
	}
	responseBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(proof)
	if err != nil {
		return "", ErrInvalidResponse
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("mzgui.framework.bootstrap.v1\x00"))
	_, _ = hash.Write(requestBytes)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(responseBytes)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// VerifyResponse 在客户端保存会话前验证响应与原请求、预期 agentd 身份完全绑定。
func VerifyResponse(request *bootstrapv1.BootstrapHandshakeRequest, response *bootstrapv1.BootstrapHandshakeResponse, expected ProcessIdentity, now time.Time) error {
	if request == nil || response == nil || !response.GetAccepted() || response.GetNegotiatedProtocol() == nil || response.GetServer() == nil ||
		response.GetCompatibility() == nil || len(response.GetServerNonce()) != 32 || len(response.GetSessionToken()) < 32 ||
		!identifierPattern.MatchString(response.GetSessionId()) || !validSHA256(response.GetHandshakeSha256()) || response.GetHealthStatus() != "ready" && response.GetHealthStatus() != "degraded" {
		return fmt.Errorf("%w: 响应字段不完整", ErrInvalidResponse)
	}
	server, err := identityFromProto(response.GetServer())
	if err != nil || !matchesExpectedIdentity(server, expected) {
		return fmt.Errorf("%w: 服务端身份不匹配", ErrInvalidResponse)
	}
	protocol := response.GetNegotiatedProtocol()
	compatibility := response.GetCompatibility()
	if protocol.GetMajor() != request.GetProtocol().GetMajor() || protocol.GetMinor() > request.GetProtocol().GetMinor() ||
		compatibility.GetMajor() != protocol.GetMajor() || protocol.GetMinor() < compatibility.GetMinimumMinor() || protocol.GetMinor() > compatibility.GetMaximumMinor() {
		return fmt.Errorf("%w: 协议范围不匹配", ErrInvalidResponse)
	}
	expiresAt := time.UnixMilli(response.GetExpiresAtUnixMs()).UTC()
	if !expiresAt.After(now.UTC()) || expiresAt.After(now.UTC().Add(24*time.Hour)) ||
		!sameStringSet(response.GetGrantedCapabilities(), request.GetRequestedCapabilities()) {
		return fmt.Errorf("%w: 会话期限或 capability 不匹配", ErrInvalidResponse)
	}
	want, err := ComputeHandshakeDigest(request, response)
	if err != nil || subtle.ConstantTimeCompare([]byte(want), []byte(response.GetHandshakeSha256())) != 1 {
		return fmt.Errorf("%w: 握手摘要不匹配", ErrInvalidResponse)
	}
	return nil
}

func (service *Service) consumeNonce(identity ProcessIdentity, nonce []byte, now time.Time) bool {
	digest := sha256.Sum256(append(append([]byte(identity.ProcessRole+"\x00"+identity.InstanceID+"\x00"), nonce...), byte(0)))
	key := hex.EncodeToString(digest[:])
	service.nonceMu.Lock()
	defer service.nonceMu.Unlock()
	for current, consumedAt := range service.nonces {
		if !consumedAt.Add(service.config.NonceTTL).After(now) {
			delete(service.nonces, current)
		}
	}
	if _, exists := service.nonces[key]; exists || len(service.nonces) >= service.config.MaxNonces {
		return false
	}
	service.nonces[key] = now
	return true
}

func grantCapabilities(requested []string, allowed map[string]struct{}) ([]string, bool) {
	if len(requested) == 0 || len(requested) > securitysession.MaxCapabilities {
		return nil, false
	}
	seen := make(map[string]struct{}, len(requested))
	for _, capability := range requested {
		if !capabilityPattern.MatchString(capability) {
			return nil, false
		}
		if _, exists := allowed[capability]; !exists {
			return nil, false
		}
		if _, exists := seen[capability]; exists {
			return nil, false
		}
		seen[capability] = struct{}{}
	}
	result := append([]string(nil), requested...)
	sort.Strings(result)
	return result, true
}

func identityFromProto(value *bootstrapv1.ProcessIdentity) (ProcessIdentity, error) {
	if value == nil || value.GetApplication() == nil {
		return ProcessIdentity{}, ErrInvalidResponse
	}
	identity := ProcessIdentity{
		ProcessRole: value.GetProcessRole(), ProcessName: value.GetProcessName(), ProcessVersion: value.GetProcessVersion(),
		BuildID: value.GetBuildId(), InstanceID: value.GetInstanceId(), OS: value.GetOs(), Arch: value.GetArch(),
		ReleaseChannel: value.GetReleaseChannel(), EndpointID: value.GetEndpointId(), Application: ApplicationIdentity{
			AppID: value.GetApplication().GetAppId(), FrameworkAPIVersion: value.GetApplication().GetFrameworkApiVersion(),
			FrameworkCatalogRevision: value.GetApplication().GetFrameworkCatalogRevision(), FrameworkCatalogSHA256: value.GetApplication().GetFrameworkCatalogSha256(),
			AppManifestSHA256: value.GetApplication().GetAppManifestSha256(), FrameworkSelectionSHA256: value.GetApplication().GetFrameworkSelectionSha256(),
		},
	}
	if err := validateIdentity(identity, true); err != nil {
		return ProcessIdentity{}, err
	}
	return identity, nil
}

func validateIdentity(identity ProcessIdentity, requireInstance bool) error {
	validOS := identity.OS == "darwin" || identity.OS == "windows" || identity.OS == "linux"
	validArch := identity.Arch == "arm64" || identity.Arch == "amd64"
	validChannel := identity.ReleaseChannel == "dev" || identity.ReleaseChannel == "beta" || identity.ReleaseChannel == "stable"
	if !processPattern.MatchString(identity.ProcessRole) || !processPattern.MatchString(identity.ProcessName) ||
		!semverPattern.MatchString(identity.ProcessVersion) || !identifierPattern.MatchString(identity.BuildID) ||
		(requireInstance && !identifierPattern.MatchString(identity.InstanceID)) || !validOS || !validArch || !validChannel ||
		!processPattern.MatchString(identity.EndpointID) || !validApplication(identity.Application) {
		return ErrInvalidResponse
	}
	return nil
}

func validApplication(identity ApplicationIdentity) bool {
	return identifierPattern.MatchString(identity.AppID) && semverPattern.MatchString(identity.FrameworkAPIVersion) &&
		identity.FrameworkCatalogRevision > 0 && validSHA256(identity.FrameworkCatalogSHA256) &&
		validSHA256(identity.AppManifestSHA256) && validSHA256(identity.FrameworkSelectionSHA256)
}

func sameApplication(left, right ApplicationIdentity) bool {
	return left == right
}

func matchesExpectedIdentity(actual, expected ProcessIdentity) bool {
	if expected.InstanceID == "" {
		actual.InstanceID = ""
	}
	return actual == expected
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	set := make(map[string]struct{}, len(left))
	for _, value := range left {
		if _, exists := set[value]; exists {
			return false
		}
		set[value] = struct{}{}
	}
	for _, value := range right {
		if _, exists := set[value]; !exists {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == fmt.Sprintf("%x", decoded)
}

func rejected(code string) *bootstrapv1.BootstrapHandshakeResponse {
	return &bootstrapv1.BootstrapHandshakeResponse{RejectionCode: code}
}
