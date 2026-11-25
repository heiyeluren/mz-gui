//go:build windows

package appauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"unicode/utf16"
)

type Current struct{}

type authenticodeResult struct {
	Status     string `json:"status"`
	Thumbprint string `json:"thumbprint"`
	Subject    string `json:"subject"`
	Path       string `json:"path"`
}

func (Current) Verify(ctx context.Context, path string) (Verification, error) {
	path = strings.TrimSpace(path)
	if ctx == nil || path == "" {
		return Verification{}, ErrInvalidRequest
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Verification{}, errors.Join(ErrUntrustedApplication, err)
	}
	const script = `$ErrorActionPreference='Stop';[Console]::OutputEncoding=[Text.UTF8Encoding]::new();$s=Get-AuthenticodeSignature -LiteralPath $env:MZ_GUI_VERIFY_PATH;[pscustomobject]@{status=[string]$s.Status;thumbprint=[string]$s.SignerCertificate.Thumbprint;subject=[string]$s.SignerCertificate.Subject;path=[string]$s.Path}|ConvertTo-Json -Compress`
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	command.Env = append(os.Environ(), "MZ_GUI_VERIFY_PATH="+path)
	output, err := command.Output()
	if err != nil {
		return Verification{}, errors.Join(ErrUntrustedApplication, err)
	}
	result := authenticodeResult{}
	output = bytes.TrimPrefix(output, []byte{0xef, 0xbb, 0xbf})
	if json.Unmarshal(output, &result) != nil || !strings.EqualFold(result.Status, "Valid") || strings.TrimSpace(result.Thumbprint) == "" {
		return Verification{}, ErrUntrustedApplication
	}
	return Verification{Valid: true, Identifier: filepathBaseWithoutExtension(result.Path),
		SignerIdentity: strings.ToUpper(strings.TrimSpace(result.Thumbprint)), Publisher: strings.TrimSpace(result.Subject),
		Source: "windows-authenticode"}, nil
}

func encodePowerShell(value string) string {
	encoded := utf16.Encode([]rune(value))
	data := make([]byte, len(encoded)*2)
	for index, item := range encoded {
		binary.LittleEndian.PutUint16(data[index*2:], item)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func filepathBaseWithoutExtension(path string) string {
	path = strings.ReplaceAll(strings.TrimSpace(path), "/", `\`)
	if index := strings.LastIndex(path, `\`); index >= 0 {
		path = path[index+1:]
	}
	if strings.HasSuffix(strings.ToLower(path), ".exe") {
		path = path[:len(path)-4]
	}
	return path
}
