//go:build !darwin

package webviewruntime

func probeNativeEngine() (engineInfo, error) {
	return engineInfo{Engine: EngineWebView2}, ErrNativeHostUnavailable
}
