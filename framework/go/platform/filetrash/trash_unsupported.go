//go:build (!darwin && !linux && !windows) || (darwin && !cgo)

package filetrash

import "context"

func moveDirectory(context.Context, string) error { return ErrUnavailable }
