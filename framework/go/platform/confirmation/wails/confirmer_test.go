package wailsconfirmation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/platform/confirmation"
)

func TestConfirmerRejectsInvalidRequestAndUnavailableApplication(t *testing.T) {
	request := confirmation.Request{Title: "确认操作", Message: "这是高风险操作", ConfirmLabel: "继续", CancelLabel: "取消"}
	if _, err := (Confirmer{}).Confirm(context.Background(), confirmation.Request{}); err == nil {
		t.Fatal("空确认内容未拒绝")
	}
	if _, err := (Confirmer{}).Confirm(context.Background(), request); !errors.Is(err, confirmation.ErrUnavailable) {
		t.Fatalf("应用未启动时错误=%v", err)
	}
}

func TestConfirmerWaitsForAsynchronousNativeCallback(t *testing.T) {
	request := confirmation.Request{Title: "确认操作", Message: "这是高风险操作", ConfirmLabel: "继续", CancelLabel: "取消"}
	confirmer := Confirmer{present: func(_ confirmation.Request, complete func(bool)) error {
		go func() {
			time.Sleep(10 * time.Millisecond)
			complete(true)
		}()
		return nil
	}}
	approved, err := confirmer.Confirm(context.Background(), request)
	if err != nil || !approved {
		t.Fatalf("异步确认结果=%v err=%v", approved, err)
	}
}

func TestConfirmerUsesWindowsFixedCallbackLabels(t *testing.T) {
	request := confirmation.Request{Title: "确认操作", Message: "这是高风险操作", ConfirmLabel: "接管", CancelLabel: "取消"}
	confirm, cancel := nativeButtonLabels("windows", request)
	if confirm != "Yes" || cancel != "No" {
		t.Fatalf("Windows 回调标签错误: confirm=%q cancel=%q", confirm, cancel)
	}
	confirm, cancel = nativeButtonLabels("darwin", request)
	if confirm != "接管" || cancel != "取消" {
		t.Fatalf("macOS 回调标签错误: confirm=%q cancel=%q", confirm, cancel)
	}
}
