package bootstrap

import (
	"context"
	"crypto/rand"
	"errors"
	securitysession "github.com/heiyeluren/mz-gui/framework/go/security/session"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/managedprocess"
	bootstrapv1 "github.com/heiyeluren/mz-gui/gen/go/mzgui/framework/bootstrap/v1"
	"google.golang.org/grpc"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

var (
	ErrInvalidClientProbe     = errors.New("Framework Bootstrap ClientProbe 配置无效")
	ErrClientProbeUnavailable = errors.New("Framework Bootstrap ClientProbe 服务不可用")
	ErrClientProbeRejected    = errors.New("Framework Bootstrap ClientProbe 握手被拒绝")
)

type HealthClient interface {
	Check(context.Context, *grpc_health_v1.HealthCheckRequest, ...grpc.CallOption) (*grpc_health_v1.HealthCheckResponse, error)
}

type HandshakeClient interface {
	Handshake(context.Context, *bootstrapv1.BootstrapHandshakeRequest, ...grpc.CallOption) (*bootstrapv1.BootstrapHandshakeResponse, error)
}

type ProbeClients struct {
	Health    HealthClient
	Bootstrap HandshakeClient
	Close     func() error
}

func NewGRPCProbeClients(connection grpc.ClientConnInterface, closer io.Closer) (ProbeClients, error) {
	if connection == nil || closer == nil {
		return ProbeClients{}, ErrInvalidClientProbe
	}
	return ProbeClients{
		Health: grpc_health_v1.NewHealthClient(connection), Bootstrap: bootstrapv1.NewBootstrapServiceClient(connection), Close: closer.Close,
	}, nil
}

type ClientProbeConfig struct {
	Open                  func(context.Context) (ProbeClients, error)
	ProtocolMajor         uint32
	ProtocolMinor         uint32
	Client                ProcessIdentity
	ExpectedServer        ProcessIdentity
	RequestedCapabilities []string
	StartedAt             time.Time
	Now                   func() time.Time
	Random                io.Reader
}

type ClientProbe struct {
	mu      sync.Mutex
	config  ClientProbeConfig
	attempt uint64
	cached  managedprocess.ProbeResult
}

func NewClientProbe(config ClientProbeConfig) (*ClientProbe, error) {
	if config.Open == nil || config.ProtocolMajor == 0 || config.StartedAt.IsZero() ||
		validateIdentity(config.Client, true) != nil || validateIdentity(config.ExpectedServer, false) != nil ||
		config.ExpectedServer.ProcessRole != "agentd" || len(config.RequestedCapabilities) == 0 || len(config.RequestedCapabilities) > securitysession.MaxCapabilities {
		return nil, ErrInvalidClientProbe
	}
	seen := make(map[string]struct{}, len(config.RequestedCapabilities))
	for _, capability := range config.RequestedCapabilities {
		if !capabilityPattern.MatchString(capability) {
			return nil, ErrInvalidClientProbe
		}
		if _, exists := seen[capability]; exists {
			return nil, ErrInvalidClientProbe
		}
		seen[capability] = struct{}{}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	config.StartedAt = config.StartedAt.UTC()
	config.RequestedCapabilities = append([]string(nil), config.RequestedCapabilities...)
	sort.Strings(config.RequestedCapabilities)
	return &ClientProbe{config: config}, nil
}

func (probe *ClientProbe) Check(ctx context.Context, request managedprocess.ProbeRequest) (managedprocess.ProbeResult, error) {
	if ctx == nil || probe == nil || !request.Phase.Valid() || request.PID < 1 || request.Attempt < 1 ||
		strings.TrimSpace(request.ProcessID) != request.ProcessID || request.ProcessID == "" || len(request.ProcessID) > 128 {
		return managedprocess.ProbeResult{}, ErrInvalidClientProbe
	}
	if err := ctx.Err(); err != nil {
		return unavailable("process.health_unavailable"), errors.Join(ErrClientProbeUnavailable, err)
	}

	probe.mu.Lock()
	defer probe.mu.Unlock()
	clients, err := probe.config.Open(ctx)
	if err != nil || clients.Health == nil || clients.Bootstrap == nil || clients.Close == nil {
		return unavailable("process.health_unavailable"), errors.Join(ErrClientProbeUnavailable, err)
	}
	defer func() { _ = clients.Close() }()
	health, err := clients.Health.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil || health == nil || health.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return unavailable("process.health_unavailable"), errors.Join(ErrClientProbeUnavailable, err)
	}
	if request.Phase == managedprocess.ProbePhaseHeartbeat && probe.attempt == request.Attempt && probe.cached.ProcessVersion != "" {
		result := cloneManagedProbeResult(probe.cached)
		result.Status = managedprocess.ProbeStatusReady
		result.ReasonCode = managedprocess.ReasonReady
		return result, nil
	}

	nonce := make([]byte, 32)
	if _, err := io.ReadFull(probe.config.Random, nonce); err != nil {
		return unavailable("process.handshake_unavailable"), errors.Join(ErrClientProbeUnavailable, err)
	}
	now := probe.config.Now().UTC()
	requestMessage := &bootstrapv1.BootstrapHandshakeRequest{
		Protocol: &bootstrapv1.ProtocolVersion{Major: probe.config.ProtocolMajor, Minor: probe.config.ProtocolMinor},
		Client:   probe.config.Client.Proto(), ClientNonce: nonce, ClientStartedAtUnixMs: probe.config.StartedAt.UnixMilli(),
		RequestedAtUnixMs: now.UnixMilli(), RequestedCapabilities: append([]string(nil), probe.config.RequestedCapabilities...),
	}
	response, err := clients.Bootstrap.Handshake(ctx, requestMessage)
	if err != nil || response == nil {
		return unavailable("process.handshake_unavailable"), errors.Join(ErrClientProbeUnavailable, err)
	}
	if !response.GetAccepted() {
		result, terminal := rejectedProbeResult(response.GetRejectionCode())
		if terminal {
			return result, nil
		}
		return result, ErrClientProbeRejected
	}
	if err := VerifyResponse(requestMessage, response, probe.config.ExpectedServer, now); err != nil {
		return managedprocess.ProbeResult{
			Status: managedprocess.ProbeStatusIncompatible, ReasonCode: "process.handshake_invalid",
		}, errors.Join(ErrInvalidResponse, err)
	}
	status := managedprocess.ProbeStatusReady
	reason := managedprocess.ReasonReady
	if response.GetHealthStatus() == "degraded" {
		status = managedprocess.ProbeStatusDegraded
		reason = managedprocess.ReasonHealthDegraded
	}
	result := managedprocess.ProbeResult{
		Status: status, ReasonCode: reason, ProcessVersion: response.GetServer().GetProcessVersion(),
		ProtocolMajor: response.GetNegotiatedProtocol().GetMajor(), ProtocolMinor: response.GetNegotiatedProtocol().GetMinor(),
		InstanceID: response.GetServer().GetInstanceId(), Capabilities: append([]string(nil), response.GetGrantedCapabilities()...),
	}
	probe.attempt = request.Attempt
	probe.cached = cloneManagedProbeResult(result)
	return result, nil
}

func unavailable(reason string) managedprocess.ProbeResult {
	return managedprocess.ProbeResult{Status: managedprocess.ProbeStatusNotReady, ReasonCode: reason}
}

func rejectedProbeResult(code string) (managedprocess.ProbeResult, bool) {
	reason := "process.handshake_rejected"
	terminal := false
	switch code {
	case "INCOMPATIBLE_PROTOCOL":
		reason, terminal = "process.protocol_incompatible", true
	case "INCOMPATIBLE_APPLICATION":
		reason, terminal = "process.application_incompatible", true
	case "INCOMPATIBLE_BUILD":
		reason, terminal = "process.build_incompatible", true
	case "UNKNOWN_ROLE", "INVALID_CLIENT", "CAPABILITY_NOT_GRANTED":
		reason, terminal = "process.identity_incompatible", true
	}
	status := managedprocess.ProbeStatusNotReady
	if terminal {
		status = managedprocess.ProbeStatusIncompatible
	}
	return managedprocess.ProbeResult{Status: status, ReasonCode: reason}, terminal
}

func cloneManagedProbeResult(result managedprocess.ProbeResult) managedprocess.ProbeResult {
	result.Capabilities = append([]string(nil), result.Capabilities...)
	return result
}
