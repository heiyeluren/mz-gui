//go:build darwin && cgo

package clipboard

/*
#cgo CFLAGS: -fblocks
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include <stdlib.h>

int mzfw_clipboard_image_png(char **output, size_t *length, size_t max_raw_bytes);
*/
import "C"

import "unsafe"

func platformReadImage() ([]byte, error) {
	var output *C.char
	var length C.size_t
	result := C.mzfw_clipboard_image_png(&output, &length, C.size_t(DefaultMaxRawImageBytes))
	switch result {
	case 2:
		return nil, ErrNoImage
	case 3:
		return nil, ErrImageTooLarge
	case 0:
	default:
		return nil, ErrReadFailed
	}
	if output == nil || length == 0 {
		return nil, ErrNoImage
	}
	defer C.free(unsafe.Pointer(output))
	if uint64(length) > DefaultMaxRawImageBytes {
		return nil, ErrImageTooLarge
	}
	return C.GoBytes(unsafe.Pointer(output), C.int(length)), nil
}
