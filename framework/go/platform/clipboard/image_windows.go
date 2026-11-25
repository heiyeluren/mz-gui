//go:build windows

package clipboard

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clipboardFormatDIB   = 8
	clipboardFormatDIBV5 = 17
)

var (
	clipboardKernel32     = windows.NewLazySystemDLL("kernel32.dll")
	clipboardGlobalLock   = clipboardKernel32.NewProc("GlobalLock")
	clipboardGlobalUnlock = clipboardKernel32.NewProc("GlobalUnlock")
	clipboardGlobalSize   = clipboardKernel32.NewProc("GlobalSize")
	clipboardCopyMemory   = clipboardKernel32.NewProc("RtlMoveMemory")
)

func platformReadImage() ([]byte, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	format := uintptr(clipboardFormatDIBV5)
	available, _, _ := clipboardFormatAvailable.Call(format)
	if available == 0 {
		format = clipboardFormatDIB
		available, _, _ = clipboardFormatAvailable.Call(format)
	}
	if available == 0 {
		return nil, ErrNoImage
	}
	if !openWindowsClipboard() {
		return nil, ErrReadFailed
	}
	defer clipboardClose.Call()
	handle, _, _ := clipboardData.Call(format)
	if handle == 0 {
		return nil, ErrReadFailed
	}
	size, _, _ := clipboardGlobalSize.Call(handle)
	if size == 0 {
		return nil, ErrReadFailed
	}
	if size > DefaultMaxRawImageBytes {
		return nil, ErrImageTooLarge
	}
	pointer, _, _ := clipboardGlobalLock.Call(handle)
	if pointer == 0 {
		return nil, ErrReadFailed
	}
	defer clipboardGlobalUnlock.Call(handle)
	dib := make([]byte, int(size))
	// 让 Windows 在原生地址之间复制，避免把系统返回的 uintptr 伪装成 Go 指针。
	clipboardCopyMemory.Call(uintptr(unsafe.Pointer(&dib[0])), pointer, size)
	return dibToPNG(dib)
}
