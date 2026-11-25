//go:build darwin

package webviewruntime

/*
#cgo LDFLAGS: -framework Foundation -framework WebKit
#include <stdlib.h>
char *mzgui_webkit_version(void);
*/
import "C"

import (
	"strings"
	"unsafe"
)

func probeNativeEngine() (engineInfo, error) {
	raw := C.mzgui_webkit_version()
	if raw == nil {
		return engineInfo{Engine: EngineWKWebView}, ErrNativeHostUnavailable
	}
	defer C.free(unsafe.Pointer(raw))
	version := strings.TrimSpace(C.GoString(raw))
	if version == "" {
		return engineInfo{Engine: EngineWKWebView}, ErrNativeHostUnavailable
	}
	return engineInfo{Engine: EngineWKWebView, Version: version, Available: true}, nil
}
