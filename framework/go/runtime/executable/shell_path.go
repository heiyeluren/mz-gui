package executable

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/processrun"
)

var ErrShellPathUnavailable = errors.New("无法读取用户终端 PATH")

type ShellPathOptions struct {
	GOOS        string
	HomeDir     string
	Environment []string
	Timeout     time.Duration
}

// LoginShellSearchPath 只读取 PATH；不导入 shell 中的凭据、HOME 或其他配置。
// 调用方决定失败时的回退，不修改进程环境或用户的 shell 配置。
func LoginShellSearchPath(ctx context.Context, options ShellPathOptions) (string, error) {
	if ctx == nil || !filepath.IsAbs(options.HomeDir) {
		return "", ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	get := func(key string) string {
		for i := len(options.Environment) - 1; i >= 0; i-- {
			k, v, ok := strings.Cut(options.Environment[i], "=")
			if ok && (k == key || options.GOOS == "windows" && strings.EqualFold(k, key)) {
				return v
			}
		}
		return ""
	}
	if options.GOOS == "windows" {
		return get("PATH"), nil
	}
	if options.GOOS != "darwin" && options.GOOS != "linux" {
		return "", ErrInvalidOptions
	}
	shell := get("SHELL")
	if shell == "" {
		shell = "/bin/bash"
		if options.GOOS == "darwin" {
			shell = "/bin/zsh"
		}
	}
	if !filepath.IsAbs(shell) {
		return "", ErrShellPathUnavailable
	}
	const marker = "\x00MZ_EXECUTABLE_PATH\x00"
	command := `printf '\000MZ_EXECUTABLE_PATH\000%s\000' "$PATH"`
	switch filepath.Base(shell) {
	case "zsh", "bash", "sh", "dash", "ksh":
	case "fish":
		command = `printf '\000MZ_EXECUTABLE_PATH\000%s\000' (string join : $PATH)`
	default:
		return "", ErrShellPathUnavailable
	}
	timeout := options.Timeout
	if timeout <= 0 || timeout > 3*time.Second {
		timeout = 3 * time.Second
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result := processrun.Run(childCtx, processrun.Spec{Executable: shell, Arguments: []string{"-ilc", command},
		Directory: options.HomeDir, Environment: append([]string(nil), options.Environment...), OutputLimit: 256 << 10})
	if err := childCtx.Err(); err != nil {
		return "", err
	}
	if result.Err != nil || result.ExitCode != 0 || result.OutputTruncated {
		return "", ErrShellPathUnavailable
	}
	output := string(result.Output)
	start := strings.LastIndex(output, marker)
	if start < 0 {
		return "", ErrShellPathUnavailable
	}
	path, _, ok := strings.Cut(output[start+len(marker):], "\x00")
	if !ok || path == "" || strings.ContainsAny(path, "\r\n") {
		return "", ErrShellPathUnavailable
	}
	return path, nil
}
