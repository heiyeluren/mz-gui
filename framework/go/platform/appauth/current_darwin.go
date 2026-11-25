//go:build darwin

package appauth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Current struct{}

func (Current) Verify(ctx context.Context, path string) (Verification, error) {
	path = strings.TrimSpace(path)
	if ctx == nil || path == "" {
		return Verification{}, ErrInvalidRequest
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
		return Verification{}, errors.Join(ErrUntrustedApplication, err)
	}
	if output, verifyErr := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", path).CombinedOutput(); verifyErr != nil {
		return Verification{}, errors.Join(ErrUntrustedApplication, verifyErr, commandOutputError(output))
	}
	details, err := exec.CommandContext(ctx, "/usr/bin/codesign", "-d", "--verbose=4", path).CombinedOutput()
	if err != nil {
		return Verification{}, errors.Join(ErrUntrustedApplication, err, commandOutputError(details))
	}
	if output, assessErr := exec.CommandContext(ctx, "/usr/sbin/spctl", "--assess", "--type", "execute", path).CombinedOutput(); assessErr != nil {
		return Verification{}, errors.Join(ErrUntrustedApplication, assessErr, commandOutputError(output))
	}
	values := parseCodeSignDetails(details)
	identifier, signer := values["Identifier"], values["TeamIdentifier"]
	if identifier == "" || signer == "" {
		return Verification{}, ErrUntrustedApplication
	}
	return Verification{Valid: true, Identifier: identifier, SignerIdentity: signer,
		Publisher: values["Authority"], Source: "macos-codesign-spctl"}, nil
}

func parseCodeSignDetails(output []byte) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			if _, exists := result[key]; !exists {
				result[key] = strings.TrimSpace(value)
			}
		}
	}
	return result
}

func commandOutputError(output []byte) error {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return nil
	}
	if len(output) > 2048 {
		output = output[:2048]
	}
	return fmt.Errorf("原生签名校验输出: %s", output)
}
