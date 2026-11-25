//go:build darwin && cgo

package clipboard

/*
#cgo CFLAGS: -fblocks
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include <stdlib.h>

int mzfw_clipboard_file_paths_json(char **output, size_t *length);
*/
import "C"

import (
	"encoding/json"
	"unsafe"
)

const maxNativeClipboardJSONBytes = 1 << 20

func platformReadFilePaths() ([]string, error) {
	var output *C.char
	var length C.size_t
	if C.mzfw_clipboard_file_paths_json(&output, &length) != 0 {
		return nil, ErrReadFailed
	}
	if output == nil {
		return []string{}, nil
	}
	defer C.free(unsafe.Pointer(output))
	if uint64(length) > maxNativeClipboardJSONBytes {
		return nil, ErrTooManyFiles
	}
	data := C.GoBytes(unsafe.Pointer(output), C.int(length))
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return nil, ErrReadFailed
	}
	return paths, nil
}
