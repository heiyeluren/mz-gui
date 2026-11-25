package fault

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

type Code string

const (
	CodeInvalidArgument   Code = "INVALID_ARGUMENT"
	CodeNotFound          Code = "NOT_FOUND"
	CodeAlreadyExists     Code = "ALREADY_EXISTS"
	CodeConflict          Code = "CONFLICT"
	CodeUnauthenticated   Code = "UNAUTHENTICATED"
	CodePermissionDenied  Code = "PERMISSION_DENIED"
	CodeCapabilityDenied  Code = "CAPABILITY_DENIED"
	CodeTimeout           Code = "TIMEOUT"
	CodeCancelled         Code = "CANCELLED"
	CodeUnavailable       Code = "UNAVAILABLE"
	CodeResourceExhausted Code = "RESOURCE_EXHAUSTED"
	CodeDataLoss          Code = "DATA_LOSS"
	CodeIncompatible      Code = "INCOMPATIBLE"
	CodeInternal          Code = "INTERNAL"
)

var (
	ErrInvalidSpec = errors.New("Framework 错误参数无效")

	reasonPattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9_]*)+$`)
	messageKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	detailKeyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	sensitiveKey      = regexp.MustCompile(`(?i)(token|secret|password|cookie|authorization|credential|private_key|api_key|path|url|command|stderr|stack)`)
	safeValuePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
	unsafePublicText  = regexp.MustCompile(`(?i)(?:/Users/|/home/|[A-Za-z]:\\|https?://|\b(?:token|secret|password|authorization)\s*[:=])`)

	allCodes = []Code{
		CodeInvalidArgument, CodeNotFound, CodeAlreadyExists, CodeConflict, CodeUnauthenticated,
		CodePermissionDenied, CodeCapabilityDenied, CodeTimeout, CodeCancelled, CodeUnavailable,
		CodeResourceExhausted, CodeDataLoss, CodeIncompatible, CodeInternal,
	}
)

type FieldIssue = model.ValidationIssue

type Spec struct {
	Code        Code
	ReasonCode  string
	MessageKey  string
	UserMessage string
	Retryable   bool
	RetryAfter  time.Duration
	FieldIssues []FieldIssue
	RequestID   string
	TraceID     string
	SafeDetails map[string]string
	Cause       error
}

type Error struct {
	code        Code
	reasonCode  string
	messageKey  string
	userMessage string
	retryable   bool
	retryAfter  time.Duration
	fieldIssues []FieldIssue
	requestID   string
	traceID     string
	safeDetails map[string]string
	cause       error
}

type PublicError struct {
	Code         Code              `json:"code"`
	ReasonCode   string            `json:"reason_code"`
	MessageKey   string            `json:"message_key"`
	UserMessage  string            `json:"user_message"`
	Retryable    bool              `json:"retryable"`
	RetryAfterMS int64             `json:"retry_after_ms,omitempty"`
	FieldIssues  []FieldIssue      `json:"field_issues,omitempty"`
	RequestID    string            `json:"request_id,omitempty"`
	TraceID      string            `json:"trace_id,omitempty"`
	SafeDetails  map[string]string `json:"safe_details,omitempty"`
}

func AllCodes() []Code {
	return append([]Code(nil), allCodes...)
}

func New(spec Spec) (*Error, error) {
	spec.ReasonCode = strings.TrimSpace(spec.ReasonCode)
	spec.MessageKey = strings.TrimSpace(spec.MessageKey)
	spec.UserMessage = strings.TrimSpace(spec.UserMessage)
	spec.RequestID = strings.TrimSpace(spec.RequestID)
	spec.TraceID = strings.TrimSpace(spec.TraceID)
	if !validCode(spec.Code) || !reasonPattern.MatchString(spec.ReasonCode) || !messageKeyPattern.MatchString(spec.MessageKey) ||
		!validPublicText(spec.UserMessage, 256) || spec.RetryAfter < 0 || spec.RetryAfter > 24*time.Hour ||
		(spec.RetryAfter > 0 && !spec.Retryable) || !validIdentifier(spec.RequestID) || !validIdentifier(spec.TraceID) ||
		!validDetails(spec.SafeDetails) || !validFieldIssues(spec.FieldIssues) {
		return nil, ErrInvalidSpec
	}
	return &Error{
		code: spec.Code, reasonCode: spec.ReasonCode, messageKey: spec.MessageKey, userMessage: spec.UserMessage,
		retryable: spec.Retryable, retryAfter: spec.RetryAfter, fieldIssues: cloneIssues(spec.FieldIssues),
		requestID: spec.RequestID, traceID: spec.TraceID, safeDetails: cloneMap(spec.SafeDetails), cause: spec.Cause,
	}, nil
}

func (failure *Error) Error() string {
	if failure == nil {
		return ""
	}
	return failure.userMessage
}

func (failure *Error) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func (failure *Error) Code() Code {
	if failure == nil {
		return ""
	}
	return failure.code
}

func (failure *Error) ReasonCode() string {
	if failure == nil {
		return ""
	}
	return failure.reasonCode
}

func Normalize(err error) *Error {
	if err == nil {
		return nil
	}
	var failure *Error
	if errors.As(err, &failure) {
		return failure
	}
	spec := Spec{Cause: err}
	switch {
	case errors.Is(err, context.Canceled):
		spec.Code, spec.ReasonCode, spec.MessageKey, spec.UserMessage = CodeCancelled, "core.operation_cancelled", "errors.operation_cancelled", "操作已取消"
	case errors.Is(err, context.DeadlineExceeded):
		spec.Code, spec.ReasonCode, spec.MessageKey, spec.UserMessage = CodeTimeout, "core.operation_timeout", "errors.operation_timeout", "操作已超时"
	default:
		spec.Code, spec.ReasonCode, spec.MessageKey, spec.UserMessage = CodeInternal, "core.unknown_internal", "errors.internal", "操作发生内部错误"
	}
	failure, newErr := New(spec)
	if newErr != nil {
		return &Error{code: CodeInternal, reasonCode: "core.unknown_internal", messageKey: "errors.internal", userMessage: "操作发生内部错误", cause: err}
	}
	return failure
}

func Public(err error) PublicError {
	failure := Normalize(err)
	if failure == nil {
		return PublicError{}
	}
	return PublicError{
		Code: failure.code, ReasonCode: failure.reasonCode, MessageKey: failure.messageKey,
		UserMessage: failure.userMessage, Retryable: failure.retryable, RetryAfterMS: failure.retryAfter.Milliseconds(),
		FieldIssues: cloneIssues(failure.fieldIssues), RequestID: failure.requestID, TraceID: failure.traceID,
		SafeDetails: cloneMap(failure.safeDetails),
	}
}

func validCode(code Code) bool {
	for _, candidate := range allCodes {
		if code == candidate {
			return true
		}
	}
	return false
}

func validIdentifier(value string) bool {
	return value == "" || identifierPattern.MatchString(value)
}

func validPublicText(value string, max int) bool {
	return value != "" && len(value) <= max && !strings.ContainsAny(value, "\r\n\x00") && !unsafePublicText.MatchString(value)
}

func validDetails(values map[string]string) bool {
	if len(values) > 16 {
		return false
	}
	for key, value := range values {
		if !detailKeyPattern.MatchString(key) || sensitiveKey.MatchString(key) || !safeValuePattern.MatchString(value) {
			return false
		}
	}
	return true
}

func validFieldIssues(issues []FieldIssue) bool {
	if len(issues) > 32 {
		return false
	}
	for _, issue := range issues {
		if issue.Validate() != nil {
			return false
		}
	}
	return true
}

func cloneIssues(values []FieldIssue) []FieldIssue {
	result := make([]FieldIssue, len(values))
	for index, issue := range values {
		result[index] = issue
		result[index].Params = cloneMap(issue.Params)
	}
	return result
}

func cloneMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func invalidCatalog(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCatalog, fmt.Sprintf(format, args...))
}
