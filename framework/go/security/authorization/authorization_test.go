package authorization

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/security/audit"
	auditmigrations "github.com/heiyeluren/mz-gui/framework/go/security/audit/migrations"
	securitysession "github.com/heiyeluren/mz-gui/framework/go/security/session"
	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	testReadMethod              = "/sample.task.v1.TaskService/GetTask"
	testWriteMethod             = "/sample.task.v1.TaskService/CreateTask"
	testSessionMetadata         = "x-sample-session-id"
	testSessionTokenMetadata    = "x-sample-session-token"
	testRequestMetadata         = "x-sample-request-id"
	testTraceMetadata           = "x-sample-trace-id"
	testCallerComponentMetadata = "x-sample-caller-component"
	testCallerVersionMetadata   = "x-sample-caller-version"
	testIdempotencyMetadata     = "x-sample-idempotency-key"
)

var testMetadataKeys = MetadataKeys{
	SessionID: testSessionMetadata, SessionToken: testSessionTokenMetadata, RequestID: testRequestMetadata, TraceID: testTraceMetadata,
	CallerComponent: testCallerComponentMetadata, CallerVersion: testCallerVersionMetadata,
	IdempotencyKey: testIdempotencyMetadata,
}

func TestUnaryAuthorizerInjectsVerifiedCaller(t *testing.T) {
	store, session := testSession(t, []string{"task.read", "task.write"})
	interceptor, err := NewUnaryAuthorizer(Options{Store: store, Metadata: testMetadataKeys, Rules: map[string]Rule{
		testReadMethod:  {Capability: "task.read"},
		testWriteMethod: {Capability: "task.write", Write: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := authorizedContext(t, session, true)
	response, err := interceptor(ctx, "request", &grpc.UnaryServerInfo{FullMethod: testWriteMethod}, func(ctx context.Context, request any) (any, error) {
		caller, ok := CallerFromContext(ctx)
		if !ok || caller.Session.ID != session.ID || caller.RequestID != "request-1" || caller.TraceID != "trace-1" ||
			caller.IdempotencyKey != "idempotency-1" || request != "request" {
			t.Fatalf("调用方上下文错误: caller=%#v ok=%v", caller, ok)
		}
		return "response", nil
	})
	if err != nil || response != "response" {
		t.Fatalf("授权调用失败: response=%v err=%v", response, err)
	}
}

func TestUnaryAuthorizerAllowsOnlyDeclaredPublicMethods(t *testing.T) {
	store, _ := testSession(t, nil)
	interceptor, err := NewUnaryAuthorizer(Options{Store: store, Metadata: testMetadataKeys, Rules: map[string]Rule{
		"/sample.system.v1.SystemService/Handshake": {Public: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/sample.system.v1.SystemService/Handshake"}, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if err != nil || !called {
		t.Fatalf("公开方法未放行: called=%v err=%v", called, err)
	}
	_, err = interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/unknown.Service/Method"}, func(context.Context, any) (any, error) {
		t.Fatal("未知方法不应进入 handler")
		return nil, nil
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("未知方法错误码 = %s", status.Code(err))
	}
}

func TestUnaryAuthorizerRejectsMissingOrMismatchedSecurityMetadata(t *testing.T) {
	store, session := testSession(t, []string{"task.read", "task.write"})
	interceptor, err := NewUnaryAuthorizer(Options{Store: store, Metadata: testMetadataKeys, Rules: map[string]Rule{
		testReadMethod:  {Capability: "task.read"},
		testWriteMethod: {Capability: "task.write", Write: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	valid := map[string]string{
		testSessionMetadata: session.ID, testSessionTokenMetadata: session.Token, testRequestMetadata: "request-1", testTraceMetadata: "trace-1",
		testCallerComponentMetadata: "desktop", testCallerVersionMetadata: "1.0.0", testIdempotencyMetadata: "idempotency-1",
	}
	tests := []struct {
		name     string
		method   string
		mutate   func(map[string]string)
		deadline bool
		code     codes.Code
	}{
		{name: "无 deadline", method: testReadMethod, deadline: false, code: codes.InvalidArgument},
		{name: "无 session", method: testReadMethod, deadline: true, mutate: func(values map[string]string) { delete(values, testSessionMetadata) }, code: codes.Unauthenticated},
		{name: "无 session token", method: testReadMethod, deadline: true, mutate: func(values map[string]string) { delete(values, testSessionTokenMetadata) }, code: codes.Unauthenticated},
		{name: "错误 session token", method: testReadMethod, deadline: true, mutate: func(values map[string]string) { values[testSessionTokenMetadata] = "wrong" }, code: codes.Unauthenticated},
		{name: "无 request id", method: testReadMethod, deadline: true, mutate: func(values map[string]string) { delete(values, testRequestMetadata) }, code: codes.InvalidArgument},
		{name: "无 trace id", method: testReadMethod, deadline: true, mutate: func(values map[string]string) { delete(values, testTraceMetadata) }, code: codes.InvalidArgument},
		{name: "调用方不匹配", method: testReadMethod, deadline: true, mutate: func(values map[string]string) { values[testCallerComponentMetadata] = "helper" }, code: codes.Unauthenticated},
		{name: "写操作无幂等键", method: testWriteMethod, deadline: true, mutate: func(values map[string]string) { delete(values, testIdempotencyMetadata) }, code: codes.InvalidArgument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := make(map[string]string, len(valid))
			for key, value := range valid {
				values[key] = value
			}
			if test.mutate != nil {
				test.mutate(values)
			}
			ctx := metadataContext(t, values, test.deadline)
			_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: test.method}, func(context.Context, any) (any, error) {
				t.Fatal("非法请求不应进入 handler")
				return nil, nil
			})
			if status.Code(err) != test.code {
				t.Fatalf("错误码 = %s, want %s, err=%v", status.Code(err), test.code, err)
			}
		})
	}
}

func TestUnaryAuthorizerRejectsCapabilityNotGranted(t *testing.T) {
	store, session := testSession(t, []string{"task.read"})
	interceptor, err := NewUnaryAuthorizer(Options{Store: store, Metadata: testMetadataKeys, Rules: map[string]Rule{
		testWriteMethod: {Capability: "task.write", Write: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = interceptor(authorizedContext(t, session, true), nil, &grpc.UnaryServerInfo{FullMethod: testWriteMethod}, func(context.Context, any) (any, error) {
		t.Fatal("越权请求不应进入 handler")
		return nil, nil
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("越权错误码 = %s", status.Code(err))
	}
}

func TestUnaryAuthorizerRecordsWriteAndDeniedDecisions(t *testing.T) {
	store, session := testSession(t, []string{"task.write"})
	audits, err := audit.NewStore(audit.StoreOptions{MaxEvents: 8})
	if err != nil {
		t.Fatal(err)
	}
	interceptor, err := NewUnaryAuthorizer(Options{
		Store: store, Audit: audits, Metadata: testMetadataKeys,
		Rules: map[string]Rule{testWriteMethod: {Capability: "task.write", Write: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := interceptor(authorizedContext(t, session, true), "request", &grpc.UnaryServerInfo{FullMethod: testWriteMethod}, func(context.Context, any) (any, error) {
		return "ok", nil
	}); err != nil {
		t.Fatal(err)
	}
	allowed := audits.List()
	if len(allowed) != 1 || allowed[0].Decision != string(audit.DecisionAllow) || allowed[0].Action != "rpc.write" || allowed[0].RequestID != "request-1" {
		t.Fatalf("写成功未进入审计: %#v", allowed)
	}
	deniedSession, err := store.Issue("desktop", "1.0.0", []string{"task.read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := interceptor(authorizedContext(t, deniedSession, true), nil, &grpc.UnaryServerInfo{FullMethod: testWriteMethod}, func(context.Context, any) (any, error) {
		t.Fatal("越权请求不应进入 handler")
		return nil, nil
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("越权错误码 = %s", status.Code(err))
	}
	records := audits.List()
	if len(records) != 2 || records[1].Decision != string(audit.DecisionDeny) || records[1].ReasonCode != "authorization.capability_denied" {
		t.Fatalf("写拒绝未进入审计: %#v", records)
	}
}

func TestUnaryAuthorizerRecordsWriteDecisionsToSQLiteAudit(t *testing.T) {
	store, session := testSession(t, []string{"task.write"})
	ctx := context.Background()
	database, err := frameworksqlite.Open(ctx, frameworksqlite.Options{
		Path:    filepath.Join(t.TempDir(), "authz-audit.db"),
		Bundles: []frameworksqlite.MigrationBundle{auditmigrations.Bundle()},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	audits := audit.NewSQLiteStore(database)
	interceptor, err := NewUnaryAuthorizer(Options{
		Store: store, Audit: audits, Metadata: testMetadataKeys,
		Rules: map[string]Rule{testWriteMethod: {Capability: "task.write", Write: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := interceptor(authorizedContext(t, session, true), "request", &grpc.UnaryServerInfo{FullMethod: testWriteMethod}, func(context.Context, any) (any, error) {
		return "ok", nil
	}); err != nil {
		t.Fatal(err)
	}
	page, err := audits.List(ctx, audit.Query{Action: "rpc.write", Capability: "task.write", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].Decision != string(audit.DecisionAllow) || page.Records[0].RequestID != "request-1" {
		t.Fatalf("写 RPC 未持久化审计: %+v", page)
	}
}

func TestUnaryAuthorizerValidatesConfiguration(t *testing.T) {
	store, _ := testSession(t, nil)
	if _, err := NewUnaryAuthorizer(Options{}); err == nil {
		t.Fatal("nil store 必须拒绝")
	}
	if _, err := NewUnaryAuthorizer(Options{Store: store, Metadata: testMetadataKeys, Rules: map[string]Rule{"bad": {Capability: "task.read"}}}); err == nil {
		t.Fatal("非法方法名必须拒绝")
	}
	if _, err := NewUnaryAuthorizer(Options{Store: store, Metadata: testMetadataKeys, Rules: map[string]Rule{"/service/method": {}}}); err == nil {
		t.Fatal("非公开规则必须声明 capability")
	}
	invalidMetadata := testMetadataKeys
	invalidMetadata.TraceID = invalidMetadata.RequestID
	if _, err := NewUnaryAuthorizer(Options{Store: store, Metadata: invalidMetadata}); err == nil {
		t.Fatal("重复 metadata key 必须拒绝")
	}
}

func testSession(t *testing.T, capabilities []string) (*securitysession.SessionStore, securitysession.Session) {
	t.Helper()
	sequence := 0
	store, err := securitysession.NewSessionStore(securitysession.StoreOptions{
		TTL: 15 * time.Minute, MaxSessions: 16,
		NewID: func() (string, error) {
			sequence++
			return fmt.Sprintf("session-%d", sequence), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.Issue("desktop", "1.0.0", capabilities)
	if err != nil {
		t.Fatal(err)
	}
	return store, session
}

func authorizedContext(t *testing.T, session securitysession.Session, write bool) context.Context {
	t.Helper()
	values := map[string]string{
		testSessionMetadata: session.ID, testSessionTokenMetadata: session.Token, testRequestMetadata: "request-1", testTraceMetadata: "trace-1",
		testCallerComponentMetadata: session.Component, testCallerVersionMetadata: session.ComponentVersion,
	}
	if write {
		values[testIdempotencyMetadata] = "idempotency-1"
	}
	return metadataContext(t, values, true)
}

func metadataContext(t *testing.T, values map[string]string, deadline bool) context.Context {
	t.Helper()
	pairs := make([]string, 0, len(values)*2)
	for key, value := range values {
		pairs = append(pairs, key, value)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(pairs...))
	if !deadline {
		return ctx
	}
	result, cancel := context.WithTimeout(ctx, time.Second)
	t.Cleanup(cancel)
	return result
}
