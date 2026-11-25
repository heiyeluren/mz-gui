//go:build !darwin && !windows

package appauth

import "context"

type Current struct{}

func (Current) Verify(context.Context, string) (Verification, error) {
	return Verification{}, ErrUntrustedApplication
}
