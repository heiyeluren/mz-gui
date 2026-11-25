package webpolicy

import (
	"strings"
	"testing"
)

func TestStrictPolicyAllowsInternalOriginAndDeniesExternalNavigation(t *testing.T) {
	policy, err := NewStrictPolicy(Options{InternalOrigin: "app://framework"})
	if err != nil {
		t.Fatal(err)
	}

	allowed := policy.EvaluateNavigation("app://framework/index.html")
	if !allowed.Allowed || allowed.ReasonCode != ReasonAllowed || allowed.MatchedRule != "internal-origin" {
		t.Fatalf("内部来源应被允许: %#v", allowed)
	}

	denied := policy.EvaluateNavigation("https://example.com/help")
	if denied.Allowed || denied.ReasonCode != ReasonHostDenied {
		t.Fatalf("未列入白名单的外部导航应被拒绝: %#v", denied)
	}
}

func TestPolicyAllowsExplicitNavigationRule(t *testing.T) {
	policy, err := NewStrictPolicy(Options{
		InternalOrigin: "app://framework",
		NavigationRules: []URLRule{
			{ID: "docs", Scheme: "https", Host: "docs.example.com", AllowSubdomains: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "精确主机", url: "https://docs.example.com/start", want: true},
		{name: "子域名", url: "https://v1.docs.example.com/start", want: true},
		{name: "错误 scheme", url: "http://docs.example.com/start", want: false},
		{name: "相邻主机", url: "https://evil-docs.example.com/start", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := policy.EvaluateNavigation(test.url)
			if decision.Allowed != test.want {
				t.Fatalf("导航判定不符合预期: url=%s decision=%#v", test.url, decision)
			}
		})
	}
}

func TestPolicyRejectsUnsafeURLsAndRules(t *testing.T) {
	policy, err := NewStrictPolicy(Options{InternalOrigin: "app://framework"})
	if err != nil {
		t.Fatal(err)
	}

	for _, rawURL := range []string{"javascript:alert(1)", "file:///Users/black/secret.txt", "data:text/html,hi", "://broken"} {
		decision := policy.EvaluateNavigation(rawURL)
		if decision.Allowed {
			t.Fatalf("危险 URL 不应被允许: url=%s decision=%#v", rawURL, decision)
		}
	}

	_, err = NewStrictPolicy(Options{
		InternalOrigin: "app://framework",
		NavigationRules: []URLRule{
			{ID: "wildcard", Scheme: "https", Host: "*"},
		},
	})
	if err == nil {
		t.Fatal("通配主机规则应被拒绝")
	}
}

func TestPolicyBuildsDeterministicCSPHeader(t *testing.T) {
	policy, err := NewStrictPolicy(Options{InternalOrigin: "app://framework"})
	if err != nil {
		t.Fatal(err)
	}

	header, err := policy.CSPHeader()
	if err != nil {
		t.Fatal(err)
	}
	wantParts := []string{
		"base-uri 'self'",
		"connect-src 'self'",
		"default-src 'self'",
		"frame-ancestors 'none'",
		"img-src 'self' data:",
		"object-src 'none'",
		"script-src 'self'",
		"style-src 'self'",
	}
	want := strings.Join(wantParts, "; ")
	if header != want {
		t.Fatalf("CSP header 不稳定:\ngot  %s\nwant %s", header, want)
	}
}

func TestDevToolsPolicyRequiresExplicitDevelopmentChannel(t *testing.T) {
	policy, err := NewStrictPolicy(Options{
		InternalOrigin: "app://framework",
		DevTools: DevToolsPolicy{
			Enabled:             true,
			AllowedEnvironments: []Environment{EnvironmentDevelopment},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !policy.AllowsDevTools(EnvironmentDevelopment) {
		t.Fatal("development 环境显式允许时应开启 DevTools")
	}
	if policy.AllowsDevTools(EnvironmentProduction) {
		t.Fatal("production 环境不应开启 DevTools")
	}

	locked, err := NewStrictPolicy(Options{InternalOrigin: "app://framework"})
	if err != nil {
		t.Fatal(err)
	}
	if locked.AllowsDevTools(EnvironmentDevelopment) {
		t.Fatal("未显式开启时任何环境都不应开启 DevTools")
	}
}
