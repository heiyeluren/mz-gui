//go:build darwin && !cgo

package clipboard

func platformReadImage() ([]byte, error) {
	return nil, ErrUnavailable
}
