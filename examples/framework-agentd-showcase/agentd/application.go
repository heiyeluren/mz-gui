package main

import (
	"context"
	"crypto/rand"
	"os"
	"time"

	taskv1 "github.com/heiyeluren/mz-gui/examples/framework-agentd-showcase/gen/task/v1"
	"github.com/heiyeluren/mz-gui/examples/framework-agentd-showcase/internal/taskexample"
	securitysession "github.com/heiyeluren/mz-gui/framework/go/security/session"
	"github.com/heiyeluren/mz-gui/framework/go/transport/bootstrap"
	"github.com/heiyeluren/mz-gui/framework/go/transport/local"
	bootstrapv1 "github.com/heiyeluren/mz-gui/gen/go/mzgui/framework/bootstrap/v1"
	"google.golang.org/grpc"
)

type application struct {
	store  *taskexample.Store
	server *local.Server
}

func newApplication(ctx context.Context, endpoint local.Endpoint) (*application, error) {
	if err := os.MkdirAll(endpoint.RunDir, 0o700); err != nil {
		return nil, err
	}
	store, err := taskexample.Open(ctx, endpoint.RunDir)
	if err != nil {
		return nil, err
	}
	sessions, err := securitysession.NewSessionStore(securitysession.StoreOptions{TTL: 15 * time.Minute, MaxSessions: 16})
	if err != nil {
		_ = store.Close(context.Background())
		return nil, err
	}
	identity := showcaseIdentity()
	bootstrapService, err := bootstrap.New(bootstrap.Config{
		ProtocolMajor: bootstrap.ProtocolMajor, ProtocolMinor: bootstrap.ProtocolMinor,
		Server: identity, Roles: map[string]bootstrap.RolePolicy{
			"desktop": {ProcessName: "showcase-desktop", Capabilities: []string{"task.read", "task.write", "task.cancel"}},
		},
		Sessions: sessions, Random: rand.Reader,
		MaxClockSkew: 2 * time.Minute, NonceTTL: 5 * time.Minute, MaxNonces: 128,
	})
	if err != nil {
		_ = store.Close(context.Background())
		return nil, err
	}
	server, err := local.NewServerWithOptions(endpoint, local.ServerOptions{
		UnaryInterceptors: []grpc.UnaryServerInterceptor{taskSessionInterceptor(sessions)},
	}, func(registrar grpc.ServiceRegistrar) {
		bootstrapv1.RegisterBootstrapServiceServer(registrar, bootstrapService)
		taskv1.RegisterTaskServiceServer(registrar, newTaskService(store.Repository, store.Engine))
	})
	if err != nil {
		_ = store.Close(context.Background())
		return nil, err
	}
	return &application{store: store, server: server}, nil
}

func (app *application) Close(ctx context.Context) error {
	return app.store.Close(ctx)
}
