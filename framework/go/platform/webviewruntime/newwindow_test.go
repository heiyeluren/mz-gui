package webviewruntime

import (
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/security/webpolicy"
)

func TestEvaluateNewWindowBlocksDangerousTargets(t *testing.T) {
	policy := mustPolicy(t)
	for _, raw := range []string{"javascript:alert(1)", "data:text/html,hi", "file:///etc/passwd", "about:blank"} {
		decision := EvaluateNewWindow(policy, raw)
		if decision.Allowed {
			t.Fatalf("危险新窗口必须拒绝: %s %#v", raw, decision)
		}
	}
	allowed := EvaluateNewWindow(policy, "https://cdn.example.com/help")
	if !allowed.Allowed || allowed.ReasonCode != webpolicy.ReasonAllowed {
		t.Fatalf("白名单 https 新窗口应允许: %#v", allowed)
	}
}
