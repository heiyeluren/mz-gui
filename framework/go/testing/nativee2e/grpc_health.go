package nativee2e

import (
	"context"
	"fmt"

	frameworklocal "github.com/heiyeluren/mz-gui/framework/go/transport/local"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

type GRPCHealthProbe struct {
	ProbeID  string
	Endpoint frameworklocal.Endpoint
	Service  string
}

func (probe GRPCHealthProbe) Name() string { return probe.ProbeID }

func (probe GRPCHealthProbe) Check(ctx context.Context) (string, error) {
	connection, err := frameworklocal.Dial(ctx, probe.Endpoint)
	if err != nil {
		return "", err
	}
	defer connection.Close()
	response, err := grpc_health_v1.NewHealthClient(connection).Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: probe.Service})
	if err != nil {
		return "", err
	}
	status := response.GetStatus().String()
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return status, fmt.Errorf("gRPC health 状态不是 SERVING: %s", status)
	}
	return status, nil
}
