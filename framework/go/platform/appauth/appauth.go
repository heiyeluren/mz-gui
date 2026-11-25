// Package appauth 提供产品无关的桌面应用原生签名校验合同。
package appauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
)

var (
	ErrInvalidRequest       = errors.New("桌面应用签名校验请求无效")
	ErrUntrustedApplication = errors.New("桌面应用未通过原生信任校验")
)

type Verification struct {
	Valid          bool
	Identifier     string
	SignerIdentity string
	Publisher      string
	Source         string
}

type Verifier interface {
	Verify(context.Context, string) (Verification, error)
}

func ValidateVerification(verification Verification, expectedIdentifier, expectedSigner string) error {
	expectedIdentifier = strings.TrimSpace(expectedIdentifier)
	expectedSigner = strings.TrimSpace(expectedSigner)
	if !verification.Valid || expectedIdentifier == "" || expectedSigner == "" || strings.TrimSpace(verification.Source) == "" ||
		!equalText(verification.Identifier, expectedIdentifier) || !equalText(verification.SignerIdentity, expectedSigner) {
		return ErrUntrustedApplication
	}
	return nil
}

func equalText(left, right string) bool {
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
