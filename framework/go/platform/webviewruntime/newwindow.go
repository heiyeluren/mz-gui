package webviewruntime

import (
	"net/url"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/security/webpolicy"
)

const ReasonNewWindowBlocked = "webview.new_window_blocked"

func EvaluateNewWindow(policy webpolicy.Policy, rawURL string) webpolicy.Decision {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" {
		return webpolicy.Decision{ReasonCode: ReasonNewWindowBlocked}
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "javascript", "data", "vbscript", "file", "about":
		return webpolicy.Decision{ReasonCode: ReasonNewWindowBlocked}
	}
	return EvaluateNavigation(policy, rawURL)
}
