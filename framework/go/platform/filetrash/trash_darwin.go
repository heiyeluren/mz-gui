//go:build darwin && cgo

package filetrash

/*
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
int mzfw_trash_directory(const char *path);
*/
import "C"

import (
	"context"
	"unsafe"
)

func moveDirectory(_ context.Context, path string) error {
	value := C.CString(path)
	defer C.free(unsafe.Pointer(value))
	if C.mzfw_trash_directory(value) != 0 {
		return ErrUnavailable
	}
	return nil
}
