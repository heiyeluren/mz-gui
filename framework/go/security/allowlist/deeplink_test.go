package allowlist

import (
	"errors"
	"testing"
)

func TestCheckDeepLinkAllowsCustomSchemeAndRejectsWebAndFile(t *testing.T) {
	if err := CheckDeepLink("mozi://settings/general", []string{"mozi"}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"javascript:alert(1)",
		"https://evil.example/x",
		"file:///etc/passwd",
		"other://x",
		"mozi://../escape",
		"mozi:javascript:alert(1)",
		"mozi:data:text/html,x",
		"mozi://settings/%2e%2e/escape",
		"mozi://user:pass@settings/general",
		"mozi://settings/general?next=javascript:alert(1)",
		"mozi://settings/general#data:text/html,x",
		"mozi://settings/general?url=https://evil.example",
		"mozi://settings/general?path=file:///etc/passwd",
		"mozi://settings/general\njavascript:alert(1)",
		"",
	} {
		if err := CheckDeepLink(raw, []string{"mozi"}); !errors.Is(err, ErrDenied) {
			t.Fatalf("Deep Link %q 必须拒绝: %v", raw, err)
		}
	}
	if err := CheckDeepLink("mozi://settings/general?tab=network", []string{"mozi"}); err != nil {
		t.Fatalf("无危险 scheme 的查询参数应允许: %v", err)
	}
}
