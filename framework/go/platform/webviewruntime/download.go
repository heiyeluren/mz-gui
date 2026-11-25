package webviewruntime

import (
	"path/filepath"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/security/webpolicy"
)

const ReasonDownloadDenied = "webview.download_denied"

var blockedDownloadExt = map[string]struct{}{
	".exe": {}, ".bat": {}, ".cmd": {}, ".com": {}, ".msi": {},
	".scr": {}, ".ps1": {}, ".sh": {}, ".dylib": {}, ".so": {},
}

type DownloadRequest struct {
	URL           string
	SuggestedName string
	MIMEType      string
}

func EvaluateDownload(policy webpolicy.Policy, request DownloadRequest) webpolicy.Decision {
	name := filepath.Base(strings.TrimSpace(request.SuggestedName))
	if name == "" || name != request.SuggestedName || strings.Contains(name, "..") {
		return webpolicy.Decision{ReasonCode: ReasonDownloadDenied}
	}
	ext := strings.ToLower(filepath.Ext(name))
	if _, blocked := blockedDownloadExt[ext]; blocked {
		return webpolicy.Decision{ReasonCode: ReasonDownloadDenied}
	}
	if strings.Contains(strings.ToLower(request.MIMEType), "executable") {
		return webpolicy.Decision{ReasonCode: ReasonDownloadDenied}
	}
	decision := policy.EvaluateResource(request.URL)
	if !decision.Allowed {
		return webpolicy.Decision{ReasonCode: ReasonDownloadDenied, MatchedRule: decision.MatchedRule}
	}
	return decision
}
