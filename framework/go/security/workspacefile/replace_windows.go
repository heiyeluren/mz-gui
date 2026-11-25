//go:build windows

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

func syncDirectory(*os.Root, string) error { return nil }
