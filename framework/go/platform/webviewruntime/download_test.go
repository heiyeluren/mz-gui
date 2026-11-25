package webviewruntime

import (
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/security/webpolicy"
)

func TestEvaluateDownloadRejectsExecutablesAndAllowsSafeAssets(t *testing.T) {
	policy := mustResourcePolicy(t)
	denied := EvaluateDownload(policy, DownloadRequest{
		URL: "https://cdn.example.com/setup.exe", SuggestedName: "setup.exe", MIMEType: "application/octet-stream",
	})
	if denied.Allowed {
		t.Fatalf("可执行下载必须拒绝: %#v", denied)
	}
	escaped := EvaluateDownload(policy, DownloadRequest{
		URL: "https://cdn.example.com/a.png", SuggestedName: "../secret.png", MIMEType: "image/png",
	})
	if escaped.Allowed {
		t.Fatal("路径穿越文件名必须拒绝")
	}
	allowed := EvaluateDownload(policy, DownloadRequest{
		URL: "https://cdn.example.com/a.png", SuggestedName: "a.png", MIMEType: "image/png",
	})
	if !allowed.Allowed {
		t.Fatalf("安全资源下载应允许: %#v", allowed)
	}
}

func mustResourcePolicy(t *testing.T) webpolicy.Policy {
	t.Helper()
	policy, err := webpolicy.NewStrictPolicy(webpolicy.Options{
		InternalOrigin: "app://desktop",
		ResourceRules:  []webpolicy.URLRule{{ID: "cdn", Scheme: "https", Host: "cdn.example.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}
