package deviceprobe

import (
	"fmt"
	"runtime"
)

type Report struct {
	GOOS   string
	GOARCH string
}

func Identify() (Report, error) {
	if runtime.GOOS == "" || runtime.GOARCH == "" {
		return Report{}, fmt.Errorf("无法识别本机平台")
	}
	return Report{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}, nil
}

func IsMacOSARM64() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

func IsMacOSAMD64() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "amd64"
}

func IsWindowsAMD64() bool {
	return runtime.GOOS == "windows" && runtime.GOARCH == "amd64"
}
