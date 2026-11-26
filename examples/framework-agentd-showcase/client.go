package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"time"

	taskv1 "github.com/heiyeluren/mz-gui/examples/framework-agentd-showcase/gen/task/v1"
	"github.com/heiyeluren/mz-gui/framework/go/transport/bootstrap"
	"github.com/heiyeluren/mz-gui/framework/go/transport/local"
	bootstrapv1 "github.com/heiyeluren/mz-gui/gen/go/mzgui/framework/bootstrap/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type agentClient struct {
	connection *grpc.ClientConn
	tasks      taskv1.TaskServiceClient
	bootstrap  bootstrapv1.BootstrapServiceClient
	sessionID  string
	token      []byte
}

func connectAgent(ctx context.Context, endpoint local.Endpoint) (*agentClient, error) {
	var lastError error
	for {
		client, err := newAgentClient(ctx, endpoint)
		if err == nil {
			return client, nil
		}
		lastError = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("连接 agentd: %w", lastError)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func newAgentClient(ctx context.Context, endpoint local.Endpoint) (*agentClient, error) {
	connection, err := local.Dial(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	client := &agentClient{
		connection: connection,
		tasks:      taskv1.NewTaskServiceClient(connection),
		bootstrap:  bootstrapv1.NewBootstrapServiceClient(connection),
	}
	if err := client.handshake(ctx); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return client, nil
}

func (client *agentClient) handshake(ctx context.Context) error {
	nonce := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	now := time.Now().UTC()
	request := &bootstrapv1.BootstrapHandshakeRequest{
		Protocol:              &bootstrapv1.ProtocolVersion{Major: bootstrap.ProtocolMajor, Minor: bootstrap.ProtocolMinor},
		Client:                showcaseClientIdentity().Proto(),
		ClientNonce:           nonce,
		ClientStartedAtUnixMs: now.Add(-time.Millisecond).UnixMilli(),
		RequestedAtUnixMs:     now.UnixMilli(),
		RequestedCapabilities: []string{"task.read", "task.write", "task.cancel"},
	}
	response, err := client.bootstrap.Handshake(ctx, request)
	if err != nil {
		return err
	}
	if !response.GetAccepted() {
		return fmt.Errorf("agentd 握手被拒绝: %s", response.GetRejectionCode())
	}
	if err := bootstrap.VerifyResponse(request, response, showcaseIdentity(), now); err != nil {
		return err
	}
	client.sessionID = response.GetSessionId()
	client.token = append([]byte(nil), response.GetSessionToken()...)
	return nil
}

func (client *agentClient) sessionContext(ctx context.Context) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("mzgui-session-id", client.sessionID, "mzgui-session-token", string(client.token)))
}

func (client *agentClient) StartTask(ctx context.Context, payload, idempotencyKey string) (TaskSnapshot, error) {
	response, err := client.tasks.StartTask(client.sessionContext(ctx), &taskv1.StartTaskRequest{Payload: payload, IdempotencyKey: idempotencyKey})
	if err != nil {
		return TaskSnapshot{}, err
	}
	return TaskSnapshot{ID: response.GetTaskId(), State: response.GetState()}, nil
}

func (client *agentClient) GetTask(ctx context.Context, taskID string) (TaskSnapshot, error) {
	response, err := client.tasks.GetTask(client.sessionContext(ctx), &taskv1.GetTaskRequest{TaskId: taskID})
	if err != nil {
		return TaskSnapshot{}, err
	}
	return TaskSnapshot{ID: response.GetTaskId(), State: response.GetState(), ResultSummaryJSON: response.GetResultSummaryJson()}, nil
}

func (client *agentClient) CancelTask(ctx context.Context, taskID, reason, idempotencyKey string) (TaskSnapshot, error) {
	response, err := client.tasks.CancelTask(client.sessionContext(ctx), &taskv1.CancelTaskRequest{TaskId: taskID, Reason: reason, IdempotencyKey: idempotencyKey})
	if err != nil {
		return TaskSnapshot{}, err
	}
	return TaskSnapshot{ID: response.GetTaskId(), State: response.GetState()}, nil
}

func (client *agentClient) Close(ctx context.Context) error {
	if client == nil || client.connection == nil {
		return nil
	}
	if client.sessionID != "" && len(client.token) > 0 {
		_, _ = client.bootstrap.RevokeSession(ctx, &bootstrapv1.SessionRevokeRequest{
			SessionId: client.sessionID, SessionToken: client.token, Reason: "client_shutdown",
		})
	}
	for index := range client.token {
		client.token[index] = 0
	}
	return client.connection.Close()
}
