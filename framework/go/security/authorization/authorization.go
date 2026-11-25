package authorization

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/security/audit"
	securitysession "github.com/heiyeluren/mz-gui/framework/go/security/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	methodPattern      = regexp.MustCompile(`^/[A-Za-z0-9_.]+/[A-Za-z0-9_]+$`)
	metadataIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	metadataKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	auditSequence      atomic.Uint64
)

type MetadataKeys struct {
	SessionID       string
	SessionToken    string
	RequestID       string
	TraceID         string
	CallerComponent string
	CallerVersion   string
	IdempotencyKey  string
}

func (keys MetadataKeys) Validate() error {
	seen := make(map[string]struct{}, 7)
	for _, key := range []string{keys.SessionID, keys.SessionToken, keys.RequestID, keys.TraceID, keys.CallerComponent, keys.CallerVersion, keys.IdempotencyKey} {
		if !metadataKeyPattern.MatchString(key) {
			return fmt.Errorf("RPC metadata key 无效")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("RPC metadata key 不能重复")
		}
		seen[key] = struct{}{}
	}
	return nil
}

type Rule struct {
	Public     bool
	Capability string
	Write      bool
}

type Caller struct {
	Session        securitysession.Session
	RequestID      string
	TraceID        string
	IdempotencyKey string
}

type callerContextKey struct{}

func CallerFromContext(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerContextKey{}).(Caller)
	return caller, ok
}

// WithCaller 供同进程服务组合与测试夹具传递已完成认证的调用方；网络入口仍必须由拦截器写入。
func WithCaller(ctx context.Context, caller Caller) context.Context {
	if ctx == nil || caller.Session.ID == "" || caller.Session.Component == "" {
		return ctx
	}
	return context.WithValue(ctx, callerContextKey{}, caller)
}

type Options struct {
	Store    *securitysession.SessionStore
	Audit    audit.Appender
	Rules    map[string]Rule
	Metadata MetadataKeys
	Now      func() time.Time
}

func NewUnaryAuthorizer(options Options) (grpc.UnaryServerInterceptor, error) {
	if options.Store == nil {
		return nil, errors.New("会话存储不能为空")
	}
	if err := options.Metadata.Validate(); err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	copyRules := make(map[string]Rule, len(options.Rules))
	for method, rule := range options.Rules {
		if !methodPattern.MatchString(method) || (rule.Public && (rule.Capability != "" || rule.Write)) ||
			(!rule.Public && !metadataIDPattern.MatchString(rule.Capability)) {
			return nil, fmt.Errorf("RPC 授权规则无效")
		}
		copyRules[method] = rule
	}
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info == nil {
			return nil, status.Error(codes.Internal, "RPC 调用信息无效")
		}
		rule, exists := copyRules[info.FullMethod]
		if !exists {
			return nil, status.Error(codes.PermissionDenied, "RPC 方法未授权")
		}
		if rule.Public {
			return handler(ctx, request)
		}
		return authorizeUnary(ctx, request, handler, options.Store, options.Audit, rule, options.Metadata, options.Now)
	}, nil
}

func authorizeUnary(
	ctx context.Context,
	request any,
	handler grpc.UnaryHandler,
	store *securitysession.SessionStore,
	audits audit.Appender,
	rule Rule,
	keys MetadataKeys,
	now func() time.Time,
) (any, error) {
	deadline, exists := ctx.Deadline()
	if !exists {
		return nil, status.Error(codes.InvalidArgument, "RPC 必须设置 deadline")
	}
	if !deadline.After(now()) {
		return nil, status.Error(codes.DeadlineExceeded, "RPC 已超过 deadline")
	}
	values, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "RPC 缺少身份信息")
	}
	sessionID, err := requiredMetadata(values, keys.SessionID, false)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "RPC 会话信息无效")
	}
	sessionToken, err := requiredMetadata(values, keys.SessionToken, false)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "RPC 会话 token 无效")
	}
	session, err := store.ValidateCredential(sessionID, sessionToken)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "RPC 会话无效或已过期")
	}
	requestID, err := requiredMetadata(values, keys.RequestID, true)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "RPC request_id 无效")
	}
	traceID, err := requiredMetadata(values, keys.TraceID, true)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "RPC trace_id 无效")
	}
	component, err := requiredMetadata(values, keys.CallerComponent, true)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "RPC 调用方无效")
	}
	version, err := requiredMetadata(values, keys.CallerVersion, true)
	if err != nil || component != session.Component || version != session.ComponentVersion {
		return nil, status.Error(codes.Unauthenticated, "RPC 调用方与会话不匹配")
	}
	if !session.Has(rule.Capability) {
		_ = recordWriteAudit(ctx, audits, session, requestID, rule, now(), audit.DecisionDeny, "authorization.capability_denied")
		return nil, status.Error(codes.PermissionDenied, "RPC capability 未授权")
	}
	idempotencyKey := ""
	if rule.Write {
		idempotencyKey, err = requiredMetadata(values, keys.IdempotencyKey, true)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "写 RPC 必须提供幂等键")
		}
	}
	if err := recordWriteAudit(ctx, audits, session, requestID, rule, now(), audit.DecisionAllow, "authorization.allowed"); err != nil {
		return nil, status.Error(codes.Internal, "RPC 审计写入失败")
	}
	caller := Caller{
		Session: session, RequestID: requestID, TraceID: traceID, IdempotencyKey: idempotencyKey,
	}
	return handler(WithCaller(ctx, caller), request)
}

func recordWriteAudit(
	ctx context.Context,
	audits audit.Appender,
	session securitysession.Session,
	requestID string,
	rule Rule,
	occurredAt time.Time,
	decision audit.Decision,
	reason string,
) error {
	if audits == nil || !rule.Write {
		return nil
	}
	suffix := "allow"
	if decision != audit.DecisionAllow {
		suffix = "deny"
	}
	requestHash := sha256.Sum256([]byte(requestID))
	_, err := audits.AppendEvent(ctx, audit.Event{
		ID:         fmt.Sprintf("authz-%x-%d-%d-%s", requestHash[:4], occurredAt.UnixNano(), auditSequence.Add(1), suffix),
		OccurredAt: occurredAt,
		Actor:      audit.Actor{Kind: audit.ActorKindProcess, ID: session.Component},
		Action:     "rpc.write",
		Capability: rule.Capability,
		Resource:   audit.Resource{Type: "rpc", ID: "write"},
		Decision:   decision,
		ReasonCode: reason,
		RequestID:  requestID,
	})
	return err
}

func requiredMetadata(values metadata.MD, key string, validateID bool) (string, error) {
	items := values.Get(key)
	if len(items) != 1 || items[0] == "" {
		return "", errors.New("metadata 缺失或重复")
	}
	if validateID && !metadataIDPattern.MatchString(items[0]) {
		return "", errors.New("metadata 格式无效")
	}
	return items[0], nil
}
