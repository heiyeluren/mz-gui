package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/managedprocess"
	bootstrapv1 "github.com/heiyeluren/mz-gui/gen/go/mzgui/framework/bootstrap/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

func TestClientProbeHandshakesOncePerProcessAttemptAndHeartbeatsWithHealth(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	clients := &recordingProbeClients{service: service, healthStatus: grpc_health_v1.HealthCheckResponse_SERVING}
	probe, err := NewClientProbe(ClientProbeConfig{
		Open: clients.Open, ProtocolMajor: ProtocolMajor, ProtocolMinor: ProtocolMinor,
		Client: clientProbeIdentity(), ExpectedServer: expectedProbeServer(),
		RequestedCapabilities: []string{"system.health", "task.read"}, StartedAt: now.Add(-time.Minute),
		Now: func() time.Time { return now }, Random: bytes.NewReader(append(bytes.Repeat([]byte{9}, 32), bytes.Repeat([]byte{10}, 32)...)),
	})
	if err != nil {
		t.Fatal(err)
	}

	readiness, err := probe.Check(context.Background(), managedprocess.ProbeRequest{
		ProcessID: "mz-gui-agentd", PID: 1001, Attempt: 1, Phase: managedprocess.ProbePhaseReadiness,
	})
	if err != nil || readiness.Status != managedprocess.ProbeStatusReady || readiness.ProcessVersion != "0.1.0" ||
		readiness.ProtocolMajor != ProtocolMajor || readiness.InstanceID != "agentd-instance-1" || len(readiness.Capabilities) != 2 {
		t.Fatalf("首次 readiness 握手异常: result=%#v err=%v", readiness, err)
	}
	heartbeat, err := probe.Check(context.Background(), managedprocess.ProbeRequest{
		ProcessID: "mz-gui-agentd", PID: 1001, Attempt: 1, Phase: managedprocess.ProbePhaseHeartbeat,
	})
	if err != nil || heartbeat.Status != managedprocess.ProbeStatusReady || heartbeat.ProcessVersion != "0.1.0" {
		t.Fatalf("同 attempt 心跳异常: result=%#v err=%v", heartbeat, err)
	}
	secondAttempt, err := probe.Check(context.Background(), managedprocess.ProbeRequest{
		ProcessID: "mz-gui-agentd", PID: 1002, Attempt: 2, Phase: managedprocess.ProbePhaseReadiness,
	})
	if err != nil || secondAttempt.Status != managedprocess.ProbeStatusReady {
		t.Fatalf("新 attempt 握手异常: result=%#v err=%v", secondAttempt, err)
	}
	if clients.HealthCalls() != 3 || clients.HandshakeCalls() != 2 || clients.CloseCalls() != 3 {
		t.Fatalf("Probe 调用次数异常: health=%d handshake=%d close=%d", clients.HealthCalls(), clients.HandshakeCalls(), clients.CloseCalls())
	}
}

func TestClientProbeClassifiesHealthRejectionAndTampering(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		configure  func(*recordingProbeClients)
		wantStatus managedprocess.ProbeStatus
		wantReason string
		wantError  bool
	}{
		{name: "健康服务未就绪", configure: func(value *recordingProbeClients) {
			value.healthStatus = grpc_health_v1.HealthCheckResponse_NOT_SERVING
		}, wantStatus: managedprocess.ProbeStatusNotReady, wantReason: "process.health_unavailable", wantError: true},
		{name: "协议不兼容", configure: func(value *recordingProbeClients) {
			value.mutate = func(response *bootstrapv1.BootstrapHandshakeResponse) {
				response.Accepted = false
				response.RejectionCode = "INCOMPATIBLE_PROTOCOL"
			}
		}, wantStatus: managedprocess.ProbeStatusIncompatible, wantReason: "process.protocol_incompatible"},
		{name: "握手摘要被篡改", configure: func(value *recordingProbeClients) {
			value.mutate = func(response *bootstrapv1.BootstrapHandshakeResponse) { response.HandshakeSha256 = "bad" }
		}, wantStatus: managedprocess.ProbeStatusIncompatible, wantReason: "process.handshake_invalid", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _ := testService(t, &now)
			clients := &recordingProbeClients{service: service, healthStatus: grpc_health_v1.HealthCheckResponse_SERVING}
			test.configure(clients)
			probe, err := NewClientProbe(ClientProbeConfig{
				Open: clients.Open, ProtocolMajor: ProtocolMajor, ProtocolMinor: ProtocolMinor,
				Client: clientProbeIdentity(), ExpectedServer: expectedProbeServer(),
				RequestedCapabilities: []string{"system.health", "task.read"}, StartedAt: now.Add(-time.Minute),
				Now: func() time.Time { return now }, Random: bytes.NewReader(bytes.Repeat([]byte{8}, 128)),
			})
			if err != nil {
				t.Fatal(err)
			}
			result, probeErr := probe.Check(context.Background(), managedprocess.ProbeRequest{
				ProcessID: "mz-gui-agentd", PID: 2001, Attempt: 1, Phase: managedprocess.ProbePhaseReadiness,
			})
			if result.Status != test.wantStatus || result.ReasonCode != test.wantReason || (probeErr != nil) != test.wantError {
				t.Fatalf("分类异常: result=%#v err=%v", result, probeErr)
			}
		})
	}
}

func TestClientProbeRejectsInvalidConfigurationAndRequest(t *testing.T) {
	now := time.Now().UTC()
	valid := ClientProbeConfig{
		Open:          func(context.Context) (ProbeClients, error) { return ProbeClients{}, errors.New("unused") },
		ProtocolMajor: ProtocolMajor, ProtocolMinor: ProtocolMinor, Client: clientProbeIdentity(), ExpectedServer: expectedProbeServer(),
		RequestedCapabilities: []string{"system.health"}, StartedAt: now.Add(-time.Minute), Now: func() time.Time { return now },
	}
	for _, mutate := range []func(*ClientProbeConfig){
		func(value *ClientProbeConfig) { value.Open = nil },
		func(value *ClientProbeConfig) { value.ProtocolMajor = 0 },
		func(value *ClientProbeConfig) { value.Client.InstanceID = "" },
		func(value *ClientProbeConfig) { value.ExpectedServer.ProcessName = "bad/name" },
		func(value *ClientProbeConfig) { value.RequestedCapabilities = nil },
		func(value *ClientProbeConfig) {
			value.RequestedCapabilities = []string{"system.health", "system.health"}
		},
		func(value *ClientProbeConfig) { value.StartedAt = time.Time{} },
	} {
		candidate := valid
		candidate.RequestedCapabilities = append([]string(nil), valid.RequestedCapabilities...)
		mutate(&candidate)
		if _, err := NewClientProbe(candidate); !errors.Is(err, ErrInvalidClientProbe) {
			t.Fatalf("非法 ClientProbe 配置必须拒绝: %v", err)
		}
	}

	probe, err := NewClientProbe(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Check(nil, managedprocess.ProbeRequest{}); !errors.Is(err, ErrInvalidClientProbe) {
		t.Fatalf("非法 Probe 请求必须拒绝: %v", err)
	}
}

func TestClientProbeRunsThroughGeneratedGRPCClients(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	bootstrapv1.RegisterBootstrapServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	probe, err := NewClientProbe(ClientProbeConfig{
		Open: func(context.Context) (ProbeClients, error) {
			connection, dialErr := grpc.NewClient("passthrough:///framework-bootstrap", grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
			if dialErr != nil {
				return ProbeClients{}, dialErr
			}
			clients, clientsErr := NewGRPCProbeClients(connection, connection)
			if clientsErr != nil {
				_ = connection.Close()
			}
			return clients, clientsErr
		},
		ProtocolMajor: ProtocolMajor, ProtocolMinor: ProtocolMinor, Client: clientProbeIdentity(), ExpectedServer: expectedProbeServer(),
		RequestedCapabilities: []string{"system.health", "task.read"}, StartedAt: now.Add(-time.Minute),
		Now: func() time.Time { return now }, Random: bytes.NewReader(bytes.Repeat([]byte{11}, 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := probe.Check(context.Background(), managedprocess.ProbeRequest{
		ProcessID: "mz-gui-agentd", PID: 3001, Attempt: 1, Phase: managedprocess.ProbePhaseReadiness,
	})
	if err != nil || result.Status != managedprocess.ProbeStatusReady || result.InstanceID != "agentd-instance-1" {
		t.Fatalf("真实 gRPC ClientProbe 异常: result=%#v err=%v", result, err)
	}
	if _, err := NewGRPCProbeClients(nil, nil); !errors.Is(err, ErrInvalidClientProbe) {
		t.Fatalf("空 gRPC ClientProbe client 必须拒绝: %v", err)
	}
}

func clientProbeIdentity() ProcessIdentity {
	identity := serverIdentity()
	identity.ProcessRole = "desktop"
	identity.ProcessName = "mz-gui-desktop"
	identity.InstanceID = "desktop-instance-1"
	return identity
}

func expectedProbeServer() ProcessIdentity {
	identity := serverIdentity()
	identity.InstanceID = ""
	return identity
}

type recordingProbeClients struct {
	mu             sync.Mutex
	service        *Service
	healthStatus   grpc_health_v1.HealthCheckResponse_ServingStatus
	mutate         func(*bootstrapv1.BootstrapHandshakeResponse)
	healthCalls    int
	handshakeCalls int
	closeCalls     int
}

func (clients *recordingProbeClients) Open(context.Context) (ProbeClients, error) {
	return ProbeClients{
		Health: healthCheckFunc(func(context.Context, *grpc_health_v1.HealthCheckRequest, ...grpc.CallOption) (*grpc_health_v1.HealthCheckResponse, error) {
			clients.mu.Lock()
			defer clients.mu.Unlock()
			clients.healthCalls++
			return &grpc_health_v1.HealthCheckResponse{Status: clients.healthStatus}, nil
		}),
		Bootstrap: handshakeFunc(func(ctx context.Context, request *bootstrapv1.BootstrapHandshakeRequest, _ ...grpc.CallOption) (*bootstrapv1.BootstrapHandshakeResponse, error) {
			clients.mu.Lock()
			clients.handshakeCalls++
			clients.mu.Unlock()
			response, err := clients.service.Handshake(ctx, request)
			if response != nil && clients.mutate != nil {
				response = proto.Clone(response).(*bootstrapv1.BootstrapHandshakeResponse)
				clients.mutate(response)
			}
			return response, err
		}),
		Close: func() error {
			clients.mu.Lock()
			defer clients.mu.Unlock()
			clients.closeCalls++
			return nil
		},
	}, nil
}

func (clients *recordingProbeClients) HealthCalls() int {
	clients.mu.Lock()
	defer clients.mu.Unlock()
	return clients.healthCalls
}
func (clients *recordingProbeClients) HandshakeCalls() int {
	clients.mu.Lock()
	defer clients.mu.Unlock()
	return clients.handshakeCalls
}
func (clients *recordingProbeClients) CloseCalls() int {
	clients.mu.Lock()
	defer clients.mu.Unlock()
	return clients.closeCalls
}

type healthCheckFunc func(context.Context, *grpc_health_v1.HealthCheckRequest, ...grpc.CallOption) (*grpc_health_v1.HealthCheckResponse, error)

func (function healthCheckFunc) Check(ctx context.Context, request *grpc_health_v1.HealthCheckRequest, options ...grpc.CallOption) (*grpc_health_v1.HealthCheckResponse, error) {
	return function(ctx, request, options...)
}

type handshakeFunc func(context.Context, *bootstrapv1.BootstrapHandshakeRequest, ...grpc.CallOption) (*bootstrapv1.BootstrapHandshakeResponse, error)

func (function handshakeFunc) Handshake(ctx context.Context, request *bootstrapv1.BootstrapHandshakeRequest, options ...grpc.CallOption) (*bootstrapv1.BootstrapHandshakeResponse, error) {
	return function(ctx, request, options...)
}
