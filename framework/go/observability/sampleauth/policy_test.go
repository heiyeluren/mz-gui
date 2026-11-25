package sampleauth

import (
	"errors"
	"testing"
)

func TestPolicySamplesDeterministicallyByTraceID(t *testing.T) {
	policy := Policy{Rate: 50}
	traceA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if policy.Sampled(traceA) != policy.Sampled(traceA) {
		t.Fatal("同一 TraceID 采样必须确定性")
	}
	always := Policy{Rate: 100}
	never := Policy{Rate: 0}
	if !always.Sampled(traceA) || never.Sampled(traceA) {
		t.Fatal("0/100 采样率边界错误")
	}
	invalid := Policy{Rate: 101}
	if invalid.Sampled(traceA) {
		t.Fatal("非法采样率不得采样")
	}
}

func TestPolicyRejectsInvalidRate(t *testing.T) {
	if err := (Policy{Rate: 101}).Validate(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("非法采样率必须拒绝: %v", err)
	}
	if err := (Policy{Rate: 10}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizeUploadRequiresCapability(t *testing.T) {
	if err := AuthorizeUpload(UploadTraces, nil); !errors.Is(err, ErrUploadDenied) {
		t.Fatalf("无 capability 必须拒绝: %v", err)
	}
	if err := AuthorizeUpload(UploadTraces, []string{"observability.upload.traces"}); err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeUpload(UploadCrashes, []string{"observability.upload.traces"}); !errors.Is(err, ErrUploadDenied) {
		t.Fatalf("错误 capability 必须拒绝: %v", err)
	}
	if err := AuthorizeUpload(UploadKind("secrets"), []string{"observability.upload.traces"}); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("未知上传类型必须拒绝: %v", err)
	}
}
