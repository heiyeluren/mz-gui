// Package remoteservice 提供产品无关的远程服务客户端端口。
package remoteservice

import (
	"errors"
	"net/http"
	"time"
)

var (
	ErrInvalidConfig                   = errors.New("RemoteService 配置无效")
	ErrPolicyDenied                    = errors.New("RemoteService 策略拒绝请求")
	ErrInvalidRequest                  = errors.New("RemoteService 请求无效")
	ErrBodyTooLarge                    = errors.New("RemoteService 响应体超过限制")
	ErrRetryExhausted                  = errors.New("RemoteService 重试次数耗尽")
	ErrInvalidSSEEvent                 = errors.New("RemoteService SSE 事件无效")
	ErrOutboxNotFound                  = errors.New("RemoteService outbox 条目不存在")
	ErrChecksumMismatch                = errors.New("RemoteService 下载校验失败")
	ErrInvalidResumeResponse           = errors.New("RemoteService 续传响应无效")
	ErrTLSPinMismatch                  = errors.New("RemoteService TLS pin 不匹配")
	ErrTLSPinPolicyExpired             = errors.New("RemoteService TLS pin 策略已过期")
	ErrTLSPinPolicyNotActive           = errors.New("RemoteService TLS pin 策略尚未生效")
	ErrInvalidReachabilityProbe        = errors.New("RemoteService 可达性探测配置无效")
	ErrInvalidUpload                   = errors.New("RemoteService 上传配置无效")
	ErrUploadFailed                    = errors.New("RemoteService 上传失败")
	ErrUploadSessionConflict           = errors.New("RemoteService 上传会话与当前文件不匹配")
	ErrInvalidOAuthConfig              = errors.New("RemoteService OAuth 配置无效")
	ErrOAuthAccessDenied               = errors.New("RemoteService OAuth 授权被拒绝")
	ErrOAuthExpired                    = errors.New("RemoteService OAuth 授权已过期")
	ErrOAuthFailed                     = errors.New("RemoteService OAuth 请求失败")
	ErrOAuthRefreshUnavailable         = errors.New("RemoteService OAuth refresh token 不可用")
	ErrInvalidProxyDiagnostic          = errors.New("RemoteService 代理诊断配置无效")
	ErrInvalidPAC                      = errors.New("RemoteService PAC 配置无效")
	ErrPACDecisionInvalid              = errors.New("RemoteService PAC 决策无效")
	ErrPACScriptUnsupported            = errors.New("RemoteService PAC 脚本不受支持")
	ErrPACFetchFailed                  = errors.New("RemoteService PAC 获取失败")
	ErrInvalidProxyCredentialRef       = errors.New("RemoteService 代理凭据引用无效")
	ErrProxyCredentialNotFound         = errors.New("RemoteService 代理凭据不存在")
	ErrProxyCredentialStoreUnavailable = errors.New("RemoteService 代理凭据库不可用")
	ErrInvalidOAuthCredentialRef       = errors.New("RemoteService OAuth 凭据引用无效")
	ErrOAuthCredentialNotFound         = errors.New("RemoteService OAuth 凭据不存在")
	ErrOAuthCredentialStoreUnavailable = errors.New("RemoteService OAuth 凭据库不可用")
)

type Policy struct {
	AllowedSchemes map[string]struct{}
	AllowedHosts   map[string]struct{}
	AllowedMethods map[string]struct{}
	AllowedHeaders map[string]struct{}
	MaxBodyBytes   int64
}

type Request struct {
	Method         string
	URL            string
	Headers        http.Header
	Body           []byte
	RequestID      string
	IdempotencyKey string
	Timeout        time.Duration
}

type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	Attempts   int
	TraceID    string
}

type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

type SSEEvent struct {
	ID    string
	Event string
	Data  string
	Retry time.Duration
}

type PayloadRef struct {
	URI       string
	SHA256    string
	SizeBytes int64
	MediaType string
}

type ReplayRequest struct {
	Method         string
	URL            string
	Headers        http.Header
	RequestID      string
	IdempotencyKey string
	Payload        PayloadRef
	Timeout        time.Duration
}

type OutboxStatus string

const (
	OutboxStatusPending   OutboxStatus = "pending"
	OutboxStatusRetrying  OutboxStatus = "retrying"
	OutboxStatusSucceeded OutboxStatus = "succeeded"
	OutboxStatusTerminal  OutboxStatus = "terminal"
)

type OutboxEntry struct {
	ID             string
	Operation      string
	Request        ReplayRequest
	DependsOn      []string
	Attempts       int
	NextAt         time.Time
	Status         OutboxStatus
	LastError      string
	ConflictPolicy string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type TransferCheckpoint struct {
	ID             string
	URL            string
	CompletedBytes int64
	TotalBytes     int64
	ETag           string
	LastModified   string
	SHA256         string
	TempPath       string
	UpdatedAt      time.Time
}
