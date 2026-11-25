package model

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestIDIsOpaqueBoundedUTF8(t *testing.T) {
	for _, value := range []string{"task-01", "会话-一"} {
		id, err := ParseID(value)
		if err != nil || id.String() != value {
			t.Fatalf("合法 ID %q 解析失败: id=%q err=%v", value, id, err)
		}
	}
	for _, value := range []string{"", " leading", "line\nbreak", string([]byte{0xff}), string(make([]byte, 129))} {
		if _, err := ParseID(value); !errors.Is(err, ErrInvalidID) {
			t.Errorf("非法 ID %q 应被拒绝，实际为 %v", value, err)
		}
	}
}

func TestTimestampAlwaysSerializesAsUTCRFC3339Nano(t *testing.T) {
	local := time.Date(2026, 8, 15, 20, 30, 10, 123456789, time.FixedZone("CST", 8*60*60))
	timestamp, err := NewTimestamp(local)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(timestamp)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"2026-08-15T12:30:10.123456789Z"` || timestamp.Time().Location() != time.UTC {
		t.Fatalf("UTC 时间序列化异常: %s %+v", data, timestamp.Time())
	}
	var decoded Timestamp
	if err := json.Unmarshal(data, &decoded); err != nil || !decoded.Equal(timestamp) {
		t.Fatalf("UTC 时间往返失败: %+v err=%v", decoded, err)
	}
	if _, err := NewTimestamp(time.Time{}); !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("零值时间应被拒绝: %v", err)
	}
}

func TestVersionUsesSemanticComparisonAndCompatibility(t *testing.T) {
	release, err := ParseVersion("1.10.0+build.7")
	if err != nil {
		t.Fatal(err)
	}
	older, _ := ParseVersion("1.9.9")
	pre, _ := ParseVersion("1.10.0-beta.2")
	if release.Compare(older) <= 0 || release.Compare(pre) <= 0 || !release.CompatibleWith(older) {
		t.Fatalf("语义版本比较错误: release=%s older=%s pre=%s", release, older, pre)
	}
	zeroMinor, _ := ParseVersion("0.4.2")
	zeroOther, _ := ParseVersion("0.5.0")
	if zeroOther.CompatibleWith(zeroMinor) {
		t.Fatal("0.x 版本不得跨 minor 声明兼容")
	}
	for _, value := range []string{"1.0", "01.0.0", "1.0.0-01", "1.0.0+"} {
		if _, err := ParseVersion(value); !errors.Is(err, ErrInvalidVersion) {
			t.Errorf("非法版本 %q 应被拒绝: %v", value, err)
		}
	}
}

func TestVersionFollowsSemanticPrereleasePrecedence(t *testing.T) {
	sequence := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta",
		"1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0",
	}
	for index := 1; index < len(sequence); index++ {
		left, leftErr := ParseVersion(sequence[index-1])
		right, rightErr := ParseVersion(sequence[index])
		if leftErr != nil || rightErr != nil || left.Compare(right) >= 0 || right.Compare(left) <= 0 {
			t.Fatalf("SemVer 顺序错误: %s < %s, leftErr=%v rightErr=%v", sequence[index-1], sequence[index], leftErr, rightErr)
		}
	}
	withBuild, _ := ParseVersion("1.2.3-rc.1+build.9")
	if withBuild.String() != "1.2.3-rc.1+build.9" {
		t.Fatalf("版本字符串往返错误: %s", withBuild)
	}
	for _, value := range []string{
		"18446744073709551616.0.0", "0.18446744073709551616.0", "0.0.18446744073709551616",
	} {
		if _, err := ParseVersion(value); !errors.Is(err, ErrInvalidVersion) {
			t.Errorf("溢出版本 %q 应被拒绝: %v", value, err)
		}
	}
}

func TestPageRequestAndPageKeepCursorOpaque(t *testing.T) {
	request, err := NewPageRequest(50, "opaque:next-1", 100)
	if err != nil || request.PageSize != 50 || request.Cursor != "opaque:next-1" {
		t.Fatalf("分页请求异常: %#v err=%v", request, err)
	}
	for _, size := range []int{0, 101} {
		if _, err := NewPageRequest(size, "", 100); !errors.Is(err, ErrInvalidPage) {
			t.Errorf("非法 page size %d 应被拒绝: %v", size, err)
		}
	}
	page, err := NewPage([]string{"a", "b"}, "opaque:next-2")
	if err != nil || !page.HasMore || page.NextCursor == "" || !reflect.DeepEqual(page.Items, []string{"a", "b"}) {
		t.Fatalf("分页结果异常: %#v err=%v", page, err)
	}
	if _, err := NewPage([]string{"a"}, "line\nbreak"); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("非法结果 cursor 应被拒绝: %v", err)
	}
}

func TestRequestMetaRequiresDeadlineCallerAndWriteIdempotency(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	deadline, _ := NewTimestamp(now.Add(time.Minute))
	version, _ := ParseVersion("1.0.0")
	meta := RequestMeta{
		RequestID: "request-1", TraceID: "trace-1", Deadline: deadline, IdempotencyKey: "write-1",
		Caller: Caller{Component: "desktop", Version: version, InstanceID: "desktop-1"},
	}
	if err := meta.Validate(now, true); err != nil {
		t.Fatalf("合法写请求元数据被拒绝: %v", err)
	}
	withoutKey := meta
	withoutKey.IdempotencyKey = ""
	if err := withoutKey.Validate(now, true); !errors.Is(err, ErrInvalidRequestMeta) {
		t.Fatalf("写请求缺少幂等键应被拒绝: %v", err)
	}
	expired := meta
	expired.Deadline, _ = NewTimestamp(now)
	if err := expired.Validate(now, false); !errors.Is(err, ErrInvalidRequestMeta) {
		t.Fatalf("已过期 deadline 应被拒绝: %v", err)
	}
	invalidKey := meta
	invalidKey.IdempotencyKey = "line\nbreak"
	if err := invalidKey.Validate(now, true); !errors.Is(err, ErrInvalidRequestMeta) {
		t.Fatalf("非法幂等键应被拒绝: %v", err)
	}
}

func TestValidationIssueRejectsFreeFormOrSensitiveParams(t *testing.T) {
	issue := ValidationIssue{Path: "profile.name", Rule: "required", MessageKey: "errors.required", Params: map[string]string{"minimum": "1"}}
	if err := issue.Validate(); err != nil {
		t.Fatalf("合法校验问题被拒绝: %v", err)
	}
	issue.Params = map[string]string{"token": "secret"}
	if err := issue.Validate(); !errors.Is(err, ErrInvalidValidationIssue) {
		t.Fatalf("敏感参数应被拒绝: %v", err)
	}
}

func TestTimestampRejectsInvalidJSONAndZeroMarshal(t *testing.T) {
	for _, data := range [][]byte{[]byte("null"), []byte("123"), []byte(`"not-a-time"`)} {
		var value Timestamp
		if err := json.Unmarshal(data, &value); !errors.Is(err, ErrInvalidTimestamp) {
			t.Errorf("非法时间 JSON %s 应被拒绝: %v", data, err)
		}
	}
	if _, err := json.Marshal(Timestamp("invalid")); !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("非法 Timestamp 不得序列化: %v", err)
	}
	if _, err := ParseTimestamp("invalid"); !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("非法时间字符串应被拒绝: %v", err)
	}
}

func TestRequestAndTraceIDRoundTripThroughContext(t *testing.T) {
	ctx := WithRequestID(nil, "request-7")
	ctx = WithTraceID(ctx, "trace-9")
	requestID, requestOK := RequestIDFromContext(ctx)
	traceID, traceOK := TraceIDFromContext(ctx)
	if !requestOK || !traceOK || requestID != "request-7" || traceID != "trace-9" {
		t.Fatalf("Context 身份往返失败: request=%q/%v trace=%q/%v", requestID, requestOK, traceID, traceOK)
	}
	if id, ok := RequestIDFromContext(WithRequestID(context.Background(), "  ")); ok || id != "" {
		t.Fatalf("空 RequestID 不得写入 Context: id=%q ok=%v", id, ok)
	}
	if _, ok := RequestIDFromContext(nil); ok {
		t.Fatal("空 Context 不得读出 RequestID")
	}
}

func TestWithRequestMetaFillsIdentityAndStoresMeta(t *testing.T) {
	now := time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)
	deadline, err := NewTimestamp(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	version, err := ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	meta := RequestMeta{
		RequestID: "request-meta", TraceID: "trace-meta", Deadline: deadline,
		Caller: Caller{Component: "desktop", Version: version, InstanceID: "desktop-1"},
	}
	ctx := WithRequestMeta(nil, meta)
	got, ok := RequestMetaFromContext(ctx)
	if !ok || got.RequestID != "request-meta" || got.Caller.Component != "desktop" {
		t.Fatalf("RequestMeta 往返失败: %#v ok=%v", got, ok)
	}
	requestID, requestOK := RequestIDFromContext(ctx)
	traceID, traceOK := TraceIDFromContext(ctx)
	if !requestOK || !traceOK || requestID != "request-meta" || traceID != "trace-meta" {
		t.Fatalf("RequestMeta 未回填身份: request=%q/%v trace=%q/%v", requestID, requestOK, traceID, traceOK)
	}
	if _, ok := RequestMetaFromContext(WithRequestMeta(context.Background(), RequestMeta{})); ok {
		t.Fatal("空 RequestMeta 不得写入 Context")
	}
	caller, callerOK := CallerFromContext(ctx)
	if !callerOK || caller.Component != "desktop" || caller.InstanceID != "desktop-1" {
		t.Fatalf("RequestMeta 未回填 Caller: %#v ok=%v", caller, callerOK)
	}
	if _, ok := CallerFromContext(WithCaller(context.Background(), Caller{})); ok {
		t.Fatal("空 Caller 不得写入 Context")
	}
}

func TestBindRequestValidatesAndAppliesDeadline(t *testing.T) {
	now := time.Date(2026, 8, 16, 15, 30, 0, 0, time.UTC)
	deadline, err := NewTimestamp(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	version, err := ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	meta := RequestMeta{
		RequestID: "request-bind", TraceID: "trace-bind", Deadline: deadline,
		Caller: Caller{Component: "desktop", Version: version, InstanceID: "desktop-1"},
	}
	ctx, cancel, err := BindRequest(nil, now, meta)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	got, ok := RequestMetaFromContext(ctx)
	if !ok || got.RequestID != "request-bind" {
		t.Fatalf("BindRequest 未写入 meta: %#v ok=%v", got, ok)
	}
	ctxDeadline, hasDeadline := ctx.Deadline()
	if !hasDeadline || !ctxDeadline.Equal(deadline.Time()) {
		t.Fatalf("BindRequest 未应用 deadline: has=%v got=%v want=%v", hasDeadline, ctxDeadline, deadline.Time())
	}
	if _, _, err := BindRequest(context.Background(), now, RequestMeta{}); !errors.Is(err, ErrInvalidRequestMeta) {
		t.Fatalf("非法 RequestMeta 必须拒绝: %v", err)
	}
}
