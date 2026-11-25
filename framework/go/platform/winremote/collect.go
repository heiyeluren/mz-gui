package winremote

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CommandResult struct {
	Code   int
	Stdout string
	Stderr string
}

type Transport interface {
	Run(ctx context.Context, argv []string) (CommandResult, error)
	Pull(ctx context.Context, remotePath, localPath string) error
}

type Options struct {
	RemoteRepo   string
	RemoteOutput string
	LocalOutput  string
	Commit       string
}

func Collect(ctx context.Context, transport Transport, options Options) (Evidence, error) {
	if transport == nil {
		return Evidence{}, fmt.Errorf("%w: 缺少远端传输", ErrRemoteCollect)
	}
	remoteRepo := strings.TrimSpace(options.RemoteRepo)
	localOutput := strings.TrimSpace(options.LocalOutput)
	if remoteRepo == "" || localOutput == "" {
		return Evidence{}, fmt.Errorf("%w: RemoteRepo 与 LocalOutput 不能为空", ErrRemoteCollect)
	}
	remoteOutput := strings.TrimSpace(options.RemoteOutput)
	if remoteOutput == "" {
		remoteOutput = joinWindows(remoteRepo, ".mzgui-windows-evidence")
	}
	script := joinWindows(remoteRepo, `scripts\windows-remote\Invoke-MzGuiWindowsDevicePack.ps1`)
	argv := []string{
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", script,
		"-RepoRoot", remoteRepo,
		"-OutputDir", remoteOutput,
	}
	if commit := strings.TrimSpace(options.Commit); commit != "" {
		argv = append(argv, "-Commit", commit)
	}
	if _, err := transport.Run(ctx, argv); err != nil {
		return Evidence{}, fmt.Errorf("%w: %v", ErrRemoteCollect, err)
	}
	if err := os.MkdirAll(filepath.Dir(localOutput), 0o755); err != nil {
		return Evidence{}, fmt.Errorf("%w: %v", ErrRemoteCollect, err)
	}
	remoteEvidence := joinWindows(remoteOutput, "windows-x64-evidence.json")
	if err := transport.Pull(ctx, remoteEvidence, localOutput); err != nil {
		return Evidence{}, fmt.Errorf("%w: %v", ErrRemoteCollect, err)
	}
	data, err := os.ReadFile(localOutput)
	if err != nil {
		return Evidence{}, fmt.Errorf("%w: %v", ErrRemoteCollect, err)
	}
	evidence, err := Decode(data)
	if err != nil {
		return Evidence{}, err
	}
	if expected := strings.TrimSpace(options.Commit); expected != "" && evidence.Commit != expected {
		return evidence, fmt.Errorf("%w: 期望 %s 实际 %s", ErrCommitMismatch, expected, evidence.Commit)
	}
	return evidence, nil
}

func joinWindows(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ReplaceAll(strings.TrimSpace(part), "/", `\`)
		part = strings.Trim(part, `\`)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	if strings.Contains(parts[0], ":") {
		return strings.Join(cleaned, `\`)
	}
	return strings.Join(cleaned, `\`)
}
