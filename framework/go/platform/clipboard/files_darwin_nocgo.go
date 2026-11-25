//go:build darwin && !cgo

package clipboard

func platformReadFilePaths() ([]string, error) {
	return nil, ErrUnavailable
}
