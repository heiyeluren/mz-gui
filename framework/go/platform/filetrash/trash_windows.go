//go:build windows

package filetrash

import (
	"context"
	"golang.org/x/sys/windows"
	"runtime"
	"syscall"
	"unsafe"
)

// IFileOperation 使用 RECYCLEONDELETE 强制回收，禁止永久删除回退。
func moveDirectory(_ context.Context, path string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole := windows.NewLazySystemDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2)
	if int32(hr) < 0 {
		return ErrUnavailable
	}
	defer ole.NewProc("CoUninitialize").Call()
	clsid, _ := windows.GUIDFromString("{3ad05575-8857-4850-9277-11b85bdb8e09}")
	iid, _ := windows.GUIDFromString("{947aab5f-0a5c-4c13-b4d6-4bf7836fc9f8}")
	var operation uintptr
	hr, _, _ = ole.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&clsid)), 0, 1, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&operation)))
	if int32(hr) < 0 || operation == 0 {
		return ErrUnavailable
	}
	defer comCall(operation, 2)
	// SILENT | NOCONFIRMATION | NOERRORUI | RECYCLEONDELETE | EARLYFAILURE。
	if comCall(operation, 5, 0x4|0x10|0x400|0x80000|0x100000) < 0 {
		return ErrUnavailable
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	shellIID, _ := windows.GUIDFromString("{43826d1e-e718-42ee-bc55-a1e261c37bfe}")
	var item uintptr
	hr, _, _ = windows.NewLazySystemDLL("shell32.dll").NewProc("SHCreateItemFromParsingName").Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&shellIID)), uintptr(unsafe.Pointer(&item)))
	if int32(hr) < 0 || item == 0 {
		return ErrUnavailable
	}
	defer comCall(item, 2)
	if comCall(operation, 18, item, 0) < 0 || comCall(operation, 21) < 0 {
		return ErrUnavailable
	}
	var aborted int32
	if comCall(operation, 22, uintptr(unsafe.Pointer(&aborted))) < 0 || aborted != 0 {
		return ErrUnavailable
	}
	return nil
}

func comCall(object uintptr, method int, args ...uintptr) int32 {
	vtable := *(**[23]uintptr)(unsafe.Pointer(object))
	result, _, _ := syscall.SyscallN(vtable[method], append([]uintptr{object}, args...)...)
	return int32(result)
}
