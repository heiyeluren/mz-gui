package testkit

import (
	"errors"
	"os"
)

const (
	FaultDiskFull           = "disk.full"
	FaultDiskCorrupt        = "disk.corrupt"
	FaultPermissionDenied   = "permission.denied"
	FaultNetworkTimeout     = "network.timeout"
	FaultNetworkUnreachable = "network.unreachable"
	FaultCanceled           = "context.canceled"
	FaultProcessCrash       = "process.crash"
	FaultWebViewCrash       = "webview.crash"
	FaultPluginHang         = "plugin.hang"
	FaultResourceLimit      = "resource.limit"
)

var (
	ErrDiskCorrupt        = errors.New("disk corrupted")
	ErrNetworkUnreachable = errors.New("network is unreachable")
	ErrProcessCrash       = errors.New("process crashed")
	ErrWebViewCrash       = errors.New("webview crashed")
	ErrPluginHang         = errors.New("plugin hung")
	ErrResourceLimit      = errors.New("resource limit exceeded")
)

func FaultError(kind string) error {
	switch kind {
	case FaultDiskFull:
		return errors.New("no space left on device")
	case FaultDiskCorrupt:
		return ErrDiskCorrupt
	case FaultPermissionDenied:
		return os.ErrPermission
	case FaultNetworkTimeout:
		return os.ErrDeadlineExceeded
	case FaultNetworkUnreachable:
		return ErrNetworkUnreachable
	case FaultCanceled:
		return os.ErrClosed
	case FaultProcessCrash:
		return ErrProcessCrash
	case FaultWebViewCrash:
		return ErrWebViewCrash
	case FaultPluginHang:
		return ErrPluginHang
	case FaultResourceLimit:
		return ErrResourceLimit
	default:
		return ErrFaultInjected
	}
}

func (injector *FaultInjector) FailKind(operation, kind string, times int) {
	injector.Fail(operation, times, FaultError(kind))
}
