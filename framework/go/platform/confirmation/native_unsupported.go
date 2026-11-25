//go:build !darwin && !windows

package confirmation

import "context"

func nativeConfirm(context.Context, Request) (bool, error) {
	return false, ErrUnavailable
}
