package ossv4

import (
	"net/http"
	"testing"
	"time"
)

func TestSignPUTMatchesAliyunSDKVector(t *testing.T) {
	request, err := http.NewRequest(http.MethodPut,
		"http://bucket.oss-cn-hangzhou.aliyuncs.com/1234%2B-/123/1.txt?param1=value1&%2Bparam1=value3&%7Cparam1=value4&%2Bparam2=&%7Cparam2=&param2=", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = SignPUT(request, Options{
		Bucket: "bucket", ObjectKey: "1234+-/123/1.txt", Region: "oss-cn-hangzhou",
		Credentials:     Credentials{AccessKeyID: "ak", AccessKeySecret: "sk", SecurityToken: "token"},
		RequiredHeaders: map[string]string{"content-type": "text/plain", "x-oss-head1": "value"},
		ContentLength:   1, SignTime: time.Unix(1_702_784_856, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	const expected = "OSS4-HMAC-SHA256 Credential=ak/20231217/cn-hangzhou/oss/aliyun_v4_request,Signature=b94a3f999cf85bcdc00d332fbd3734ba03e48382c36fa4d5af5df817395bd9ea"
	if request.Header.Get("Authorization") != expected {
		t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
	}
	if request.Header.Get("x-oss-security-token") != "token" || request.Header.Get("x-oss-content-sha256") != unsignedPayload ||
		request.Header.Get("Content-Length") != "1" || request.ContentLength != 1 {
		t.Fatalf("签名请求头不完整: %#v", request.Header)
	}
}

func TestSignPUTRejectsReservedHeadersAndInvalidInput(t *testing.T) {
	base := func() *http.Request {
		request, err := http.NewRequest(http.MethodPut, "https://bucket.oss-cn-hangzhou.aliyuncs.com/safe.zip", nil)
		if err != nil {
			t.Fatal(err)
		}
		return request
	}
	options := Options{Bucket: "bucket", ObjectKey: "safe.zip", Region: "cn-hangzhou",
		Credentials: Credentials{AccessKeyID: "ak", AccessKeySecret: "sk", SecurityToken: "token"}, ContentLength: 1}
	for _, header := range []string{"Authorization", "Host", "x-oss-date", "Date"} {
		candidate := options
		candidate.RequiredHeaders = map[string]string{header: "untrusted"}
		if err := SignPUT(base(), candidate); err == nil {
			t.Fatalf("应拒绝保留头 %q", header)
		}
	}
	for _, mutate := range []func(*Options){
		func(value *Options) { value.Bucket = "Bad_Bucket" },
		func(value *Options) { value.ObjectKey = "../safe.zip" },
		func(value *Options) { value.Region = "bad/region" },
		func(value *Options) { value.Credentials.AccessKeySecret = "" },
		func(value *Options) { value.ContentLength = 0 },
	} {
		candidate := options
		mutate(&candidate)
		if err := SignPUT(base(), candidate); err == nil {
			t.Fatal("应拒绝无效签名参数")
		}
	}
}

func TestNormalizeRegion(t *testing.T) {
	value, err := NormalizeRegion("oss-cn-hangzhou")
	if err != nil || value != "cn-hangzhou" {
		t.Fatalf("NormalizeRegion() = %q, %v", value, err)
	}
}
