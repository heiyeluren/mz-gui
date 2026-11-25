package wailsconfirmation

import (
	"context"
	"runtime"
	"sync"

	"github.com/heiyeluren/mz-gui/framework/go/platform/confirmation"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type dialogPresenter func(confirmation.Request, func(bool)) error

type Confirmer struct {
	present dialogPresenter
}

func (confirmer Confirmer) Confirm(ctx context.Context, request confirmation.Request) (bool, error) {
	if err := request.Validate(); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	present := confirmer.present
	if present == nil {
		present = presentWailsDialog
	}
	completed := make(chan bool, 1)
	var once sync.Once
	if err := present(request, func(approved bool) {
		once.Do(func() { completed <- approved })
	}); err != nil {
		return false, err
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case approved := <-completed:
		return approved, nil
	}
}

func presentWailsDialog(request confirmation.Request, complete func(bool)) error {
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return confirmation.ErrUnavailable
	}
	confirmLabel, cancelLabel := nativeButtonLabels(runtime.GOOS, request)
	dialog := app.Dialog.Question().SetTitle(request.Title).SetMessage(request.Message)
	cancel := dialog.AddButton(cancelLabel).SetAsCancel().OnClick(func() { complete(false) })
	confirm := dialog.AddButton(confirmLabel).SetAsDefault().OnClick(func() { complete(true) })
	dialog.SetCancelButton(cancel).SetDefaultButton(confirm).Show()
	return nil
}

func nativeButtonLabels(goos string, request confirmation.Request) (confirm, cancel string) {
	if goos == "windows" {
		// Wails beta.8 使用 Win32 MB_YESNO，并按固定英文返回值查找回调。
		return "Yes", "No"
	}
	return request.ConfirmLabel, request.CancelLabel
}
