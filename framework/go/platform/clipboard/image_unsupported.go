//go:build !darwin && !windows

package clipboard

func platformReadImage() ([]byte, error) {
	return nil, ErrUnavailable
}
