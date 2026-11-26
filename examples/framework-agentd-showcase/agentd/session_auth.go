package main

import (
	"context"
	"strings"

	securitysession "github.com/heiyeluren/mz-gui/framework/go/security/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func taskSessionInterceptor(sessions *securitysession.SessionStore) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !strings.HasPrefix(info.FullMethod, "/mzgui.showcase.task.v1.TaskService/") {
			return handler(ctx, request)
		}
		values, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "缺少会话元数据")
		}
		session, err := sessions.ValidateCredential(firstMetadata(values, "mzgui-session-id"), firstMetadata(values, "mzgui-session-token"))
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "会话无效或已过期")
		}
		capability := "task.read"
		switch info.FullMethod {
		case "/mzgui.showcase.task.v1.TaskService/StartTask":
			capability = "task.write"
		case "/mzgui.showcase.task.v1.TaskService/CancelTask":
			capability = "task.cancel"
		}
		if !session.Has(capability) {
			return nil, status.Error(codes.PermissionDenied, "会话缺少任务能力")
		}
		return handler(ctx, request)
	}
}

func firstMetadata(values metadata.MD, key string) string {
	items := values.Get(key)
	if len(items) == 0 {
		return ""
	}
	return items[0]
}
