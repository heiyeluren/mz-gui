package webviewruntime

import (
	"net/url"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/security/webpolicy"
)

const (
	ReasonSchemeBlocked = "webview.scheme_blocked"
	ReasonFileBlocked   = "webview.file_blocked"
)

func EvaluateNavigation(policy webpolicy.Policy, rawURL string) webpolicy.Decision {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" {
		return webpolicy.Decision{ReasonCode: webpolicy.ReasonInvalidURL}
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "javascript", "data", "vbscript":
		return webpolicy.Decision{ReasonCode: ReasonSchemeBlocked}
	case "file":
		return webpolicy.Decision{ReasonCode: ReasonFileBlocked}
	}
	return policy.EvaluateNavigation(rawURL)
}
