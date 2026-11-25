//go:build !darwin && !windows

package clipboard

func platformReadFilePaths() ([]string, error) {
	return nil, ErrUnavailable
}
