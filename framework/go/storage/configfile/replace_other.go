//go:build !darwin && !windows

package configfile

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
