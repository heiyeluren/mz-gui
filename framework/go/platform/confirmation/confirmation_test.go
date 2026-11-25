package confirmation

import (
	"context"
	"testing"
)

func TestRequestValidation(t *testing.T) {
	request := Request{Title: "确认操作", Message: "这是高风险操作", ConfirmLabel: "继续", CancelLabel: "取消"}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Request{}).Validate(); err == nil {
		t.Fatal("空确认内容未拒绝")
	}
}

func TestNativeConfirmerRejectsInvalidRequestBeforeOpeningDialog(t *testing.T) {
	if _, err := (NativeConfirmer{}).Confirm(context.Background(), Request{}); err == nil {
		t.Fatal("原生确认器未拒绝空请求")
	}
}
