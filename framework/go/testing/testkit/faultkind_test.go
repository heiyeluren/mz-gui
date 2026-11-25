package testkit

import (
	"errors"
	"os"
	"testing"
)

func TestFaultInjectorFailKindUsesNamedErrors(t *testing.T) {
	injector := NewFaultInjector()
	injector.FailKind("sqlite.write", FaultPermissionDenied, 1)
	if err := injector.Check("sqlite.write"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("权限故障错误 = %v", err)
	}
	if err := injector.Check("sqlite.write"); err != nil {
		t.Fatalf("次数耗尽后应通过: %v", err)
	}
	if FaultError(FaultDiskFull).Error() != "no space left on device" {
		t.Fatal("磁盘满故障文案错误")
	}
	if !errors.Is(FaultError(FaultNetworkTimeout), os.ErrDeadlineExceeded) {
		t.Fatal("网络超时故障必须是 DeadlineExceeded")
	}
	if !errors.Is(FaultError(FaultCanceled), os.ErrClosed) {
		t.Fatal("取消故障必须是 ErrClosed")
	}
	if !errors.Is(FaultError(FaultDiskCorrupt), ErrDiskCorrupt) {
		t.Fatal("磁盘损坏故障必须可断言")
	}
	if !errors.Is(FaultError(FaultNetworkUnreachable), ErrNetworkUnreachable) {
		t.Fatal("网络不可达故障必须可断言")
	}
	if !errors.Is(FaultError(FaultProcessCrash), ErrProcessCrash) {
		t.Fatal("进程崩溃故障必须可断言")
	}
	if !errors.Is(FaultError(FaultWebViewCrash), ErrWebViewCrash) {
		t.Fatal("WebView 崩溃故障必须可断言")
	}
	if !errors.Is(FaultError(FaultPluginHang), ErrPluginHang) {
		t.Fatal("插件挂起故障必须可断言")
	}
	if !errors.Is(FaultError(FaultResourceLimit), ErrResourceLimit) {
		t.Fatal("资源上限故障必须可断言")
	}
}
