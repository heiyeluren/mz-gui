package webviewruntime

import (
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/security/webpolicy"
)

func TestEvaluateNavigationBlocksDangerousSchemes(t *testing.T) {
	policy := mustPolicy(t)
	for _, raw := range []string{"javascript:alert(1)", "data:text/html,hi", "file:///etc/passwd"} {
		decision := EvaluateNavigation(policy, raw)
		if decision.Allowed {
			t.Fatalf("危险导航必须拒绝: %s %#v", raw, decision)
		}
	}
	allowed := EvaluateNavigation(policy, "https://cdn.example.com/app.js")
	if !allowed.Allowed || allowed.ReasonCode != webpolicy.ReasonAllowed {
		t.Fatalf("白名单 https 应允许: %#v", allowed)
	}
}

func mustPolicy(t *testing.T) webpolicy.Policy {
	t.Helper()
	policy, err := webpolicy.NewStrictPolicy(webpolicy.Options{
		InternalOrigin:  "app://desktop",
		NavigationRules: []webpolicy.URLRule{{ID: "cdn", Scheme: "https", Host: "cdn.example.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}
