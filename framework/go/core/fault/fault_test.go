package fault

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewCreatesStablePublicErrorWithoutLeakingCause(t *testing.T) {
	cause := errors.New("token=secret /Users/private/internal.sql")
	failure, err := New(Spec{
		Code: CodeUnavailable, ReasonCode: "network.remote_unavailable", MessageKey: "errors.remote_unavailable",
		UserMessage: "远程服务暂时不可用", Retryable: true, RetryAfter: 1500 * time.Millisecond,
		RequestID: "request-1", TraceID: "trace-1", SafeDetails: map[string]string{"service": "remote"}, Cause: cause,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(failure, cause) {
		t.Fatal("FrameworkError 必须保留内部 cause 链")
	}
	public := Public(failure)
	if public.Code != CodeUnavailable || public.RetryAfterMS != 1500 || !public.Retryable || public.SafeDetails["service"] != "remote" {
		t.Fatalf("公开错误字段异常: %#v", public)
	}
	encoded := failure.Error() + public.UserMessage
	if strings.Contains(encoded, "secret") || strings.Contains(encoded, "/Users/") {
		t.Fatalf("公开错误泄露内部 cause: %s", encoded)
	}
	public.SafeDetails["service"] = "changed"
	if Public(failure).SafeDetails["service"] != "remote" {
		t.Fatal("公开错误不得泄露可修改的内部 map")
	}
}

func TestNormalizeMapsContextAndUnknownErrors(t *testing.T) {
	cases := []struct {
		err    error
		code   Code
		reason string
	}{
		{err: context.Canceled, code: CodeCancelled, reason: "core.operation_cancelled"},
		{err: context.DeadlineExceeded, code: CodeTimeout, reason: "core.operation_timeout"},
		{err: errors.New("private token"), code: CodeInternal, reason: "core.unknown_internal"},
	}
	for _, test := range cases {
		failure := Normalize(test.err)
		if failure.Code() != test.code || failure.ReasonCode() != test.reason {
			t.Fatalf("Normalize(%v) = %s/%s", test.err, failure.Code(), failure.ReasonCode())
		}
		if strings.Contains(failure.Error(), "private token") {
			t.Fatal("未知错误不得透传到公开消息")
		}
	}
}

func TestNewRejectsInvalidOrSensitivePublicFields(t *testing.T) {
	base := Spec{Code: CodeInvalidArgument, ReasonCode: "core.invalid_argument", MessageKey: "errors.invalid_argument", UserMessage: "参数无效"}
	cases := []Spec{
		{Code: "UNKNOWN", ReasonCode: base.ReasonCode, MessageKey: base.MessageKey, UserMessage: base.UserMessage},
		{Code: base.Code, ReasonCode: "Bad Reason", MessageKey: base.MessageKey, UserMessage: base.UserMessage},
		{Code: base.Code, ReasonCode: base.ReasonCode, MessageKey: base.MessageKey, UserMessage: "line1\nline2"},
		{Code: base.Code, ReasonCode: base.ReasonCode, MessageKey: base.MessageKey, UserMessage: "读取 /Users/private/file 失败"},
		{Code: base.Code, ReasonCode: base.ReasonCode, MessageKey: base.MessageKey, UserMessage: base.UserMessage, SafeDetails: map[string]string{"token": "secret"}},
		{Code: base.Code, ReasonCode: base.ReasonCode, MessageKey: base.MessageKey, UserMessage: base.UserMessage, SafeDetails: map[string]string{"component": "token=secret"}},
		{Code: base.Code, ReasonCode: base.ReasonCode, MessageKey: base.MessageKey, UserMessage: base.UserMessage, FieldIssues: []FieldIssue{{Path: "", Rule: "required", MessageKey: "errors.required"}}},
	}
	for index, spec := range cases {
		if _, err := New(spec); !errors.Is(err, ErrInvalidSpec) {
			t.Fatalf("非法用例 %d 应被拒绝，实际为 %v", index, err)
		}
	}
}

func TestPublicCopiesFieldIssues(t *testing.T) {
	failure, err := New(Spec{
		Code: CodeInvalidArgument, ReasonCode: "core.invalid_argument", MessageKey: "errors.invalid_argument", UserMessage: "参数无效",
		FieldIssues: []FieldIssue{{Path: "profile.name", Rule: "required", MessageKey: "errors.required", Params: map[string]string{"minimum": "1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []FieldIssue{{Path: "profile.name", Rule: "required", MessageKey: "errors.required", Params: map[string]string{"minimum": "1"}}}
	if !reflect.DeepEqual(Public(failure).FieldIssues, want) {
		t.Fatalf("字段问题不一致: %#v", Public(failure).FieldIssues)
	}
}
