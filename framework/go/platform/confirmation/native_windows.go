//go:build windows

package confirmation

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	messageBoxYesNo       = 0x00000004
	messageBoxIconWarning = 0x00000030
	messageBoxSystemModal = 0x00001000
	messageBoxResultYes   = 6
	messageBoxResultNo    = 7
	messageBoxResultTimed = 32000
	maxMessageBoxTimeout  = 2 * time.Minute
	messageBoxDismissWait = 2 * time.Second
	buttonClickMessage    = 0x00F5
	windowCommandMessage  = 0x0111
)

var (
	user32DLL             = windows.NewLazySystemDLL("user32.dll")
	messageBoxTimeoutProc = user32DLL.NewProc("MessageBoxTimeoutW")
	findWindowProc        = user32DLL.NewProc("FindWindowW")
	getDialogItemProc     = user32DLL.NewProc("GetDlgItem")
	postMessageProc       = user32DLL.NewProc("PostMessageW")
	endDialogProc         = user32DLL.NewProc("EndDialog")
	nativeDialogSequence  atomic.Uint64
)

func nativeConfirm(ctx context.Context, request Request) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// 不可见后缀只用于精确定位本次对话框，避免取消时误关同标题窗口。
	title, err := windows.UTF16PtrFromString(request.Title + invisibleDialogSuffix(nativeDialogSequence.Add(1)))
	if err != nil {
		return false, ErrUnavailable
	}
	message, err := windows.UTF16PtrFromString(request.Message + "\n\n选择“是”执行“" + request.ConfirmLabel + "”，选择“否”执行“" + request.CancelLabel + "”。")
	if err != nil {
		return false, ErrUnavailable
	}
	timeout := nativeConfirmationTimeout(ctx)
	completed := make(chan uintptr, 1)
	go func() {
		result, _, _ := messageBoxTimeoutProc.Call(
			0,
			uintptr(unsafe.Pointer(message)),
			uintptr(unsafe.Pointer(title)),
			messageBoxYesNo|messageBoxIconWarning|messageBoxSystemModal,
			0,
			uintptr(timeout.Milliseconds()),
		)
		completed <- result
	}()

	var result uintptr
	select {
	case result = <-completed:
		if err := ctx.Err(); err != nil {
			return false, err
		}
	case <-ctx.Done():
		dismissNativeMessageBox(title, completed)
		return false, ctx.Err()
	}
	if result == 0 {
		return false, ErrUnavailable
	}
	switch result {
	case messageBoxResultYes:
		return true, nil
	case messageBoxResultNo:
		return false, nil
	case messageBoxResultTimed:
		return false, context.DeadlineExceeded
	default:
		return false, syscall.EINVAL
	}
}

func dismissNativeMessageBox(title *uint16, completed <-chan uintptr) {
	deadline := time.Now().Add(messageBoxDismissWait)
	for time.Now().Before(deadline) {
		select {
		case <-completed:
			return
		default:
		}
		window, _, _ := findWindowProc.Call(0, uintptr(unsafe.Pointer(title)))
		if window != 0 {
			button, _, _ := getDialogItemProc.Call(window, messageBoxResultNo)
			if button != 0 {
				postMessageProc.Call(button, buttonClickMessage, 0, 0)
			} else {
				postMessageProc.Call(window, windowCommandMessage, messageBoxResultNo, 0)
			}
			select {
			case <-completed:
				return
			case <-time.After(50 * time.Millisecond):
				endDialogProc.Call(window, messageBoxResultNo)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func invisibleDialogSuffix(sequence uint64) string {
	bits := strconv.FormatUint(sequence, 2)
	var suffix strings.Builder
	suffix.WriteRune('\u2063')
	for _, bit := range bits {
		if bit == '0' {
			suffix.WriteRune('\u200B')
		} else {
			suffix.WriteRune('\u200C')
		}
	}
	return suffix.String()
}

func nativeConfirmationTimeout(ctx context.Context) time.Duration {
	timeout := maxMessageBoxTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout < time.Second {
		return time.Second
	}
	return timeout
}
