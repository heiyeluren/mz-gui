//go:build !darwin && !windows

package workspacefile

import "os"

func commitFile(root *os.Root, source, destination string, replace bool) error {
	if replace {
		return root.Rename(source, destination)
	}
	if err := root.Link(source, destination); err != nil {
		return err
	}
	return root.Remove(source)
}

func syncDirectory(root *os.Root, path string) error {
	directory, err := root.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
