//go:build windows

package clipboard

import (
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const clipboardFormatFileDrop = 15

var (
	clipboardUser32          = windows.NewLazySystemDLL("user32.dll")
	clipboardShell32         = windows.NewLazySystemDLL("shell32.dll")
	clipboardOpen            = clipboardUser32.NewProc("OpenClipboard")
	clipboardClose           = clipboardUser32.NewProc("CloseClipboard")
	clipboardData            = clipboardUser32.NewProc("GetClipboardData")
	clipboardFormatAvailable = clipboardUser32.NewProc("IsClipboardFormatAvailable")
	clipboardDragQueryFile   = clipboardShell32.NewProc("DragQueryFileW")
)

func platformReadFilePaths() ([]string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	available, _, _ := clipboardFormatAvailable.Call(clipboardFormatFileDrop)
	if available == 0 {
		return []string{}, nil
	}
	if !openWindowsClipboard() {
		return nil, ErrReadFailed
	}
	defer clipboardClose.Call()
	handle, _, _ := clipboardData.Call(clipboardFormatFileDrop)
	if handle == 0 {
		return nil, ErrReadFailed
	}
	count, _, _ := clipboardDragQueryFile.Call(handle, ^uintptr(0), 0, 0)
	if count > DefaultMaxFiles {
		return nil, ErrTooManyFiles
	}
	paths := make([]string, 0, count)
	for index := uintptr(0); index < count; index++ {
		length, _, _ := clipboardDragQueryFile.Call(handle, index, 0, 0)
		if length == 0 {
			continue
		}
		buffer := make([]uint16, length+1)
		written, _, _ := clipboardDragQueryFile.Call(handle, index, uintptr(unsafe.Pointer(&buffer[0])), length+1)
		if written == 0 {
			return nil, ErrReadFailed
		}
		paths = append(paths, windows.UTF16ToString(buffer))
	}
	return paths, nil
}

func openWindowsClipboard() bool {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		opened, _, _ := clipboardOpen.Call(0)
		if opened != 0 {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
