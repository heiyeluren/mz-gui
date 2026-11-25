package terminal

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var termProgramPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var programEnvironmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

const (
	maxProgramEnvironmentEntries    = 128
	maxProgramEnvironmentValueBytes = 4096
	// 允许组合运行环境，同时维持原 32 项、每项 4 KiB 的总容量量级。
	maxProgramEnvironmentBytes = 128 << 10
)

// EnvironmentValidationError 只提供安全的校验类别和数量，不包含环境名称或值。
type EnvironmentValidationError struct {
	reason        string
	actual, limit int
}

func (err *EnvironmentValidationError) Error() string {
	switch err.reason {
	case "count":
		return fmt.Sprintf("终端环境变量数量超限（%d 项，上限 %d 项）", err.actual, err.limit)
	case "total":
		return fmt.Sprintf("终端环境变量总大小超限（%d 字节，上限 %d 字节）", err.actual, err.limit)
	case "value-size":
		return fmt.Sprintf("终端环境变量单值大小超限（上限 %d 字节）", err.limit)
	case "name":
		return "终端环境变量名称格式无效"
	case "control":
		return "终端环境变量包含不允许的控制字符"
	default:
		return "终端环境变量无效"
	}
}

func (err *EnvironmentValidationError) Unwrap() error { return ErrInvalidArgument }

type PlatformLauncherOptions struct {
	TermProgram string
}

func normalizeTermProgram(options PlatformLauncherOptions) (string, error) {
	if options.TermProgram == "" {
		return "desktop-app", nil
	}
	if !termProgramPattern.MatchString(options.TermProgram) {
		return "", fmt.Errorf("终端程序标识无效")
	}
	return options.TermProgram, nil
}

func allowedEnvironment(termProgram string, names ...string) []string {
	values := make([]string, 0, len(names)+3)
	for _, name := range names {
		if value, exists := os.LookupEnv(name); exists && value != "" {
			values = append(values, name+"="+value)
		}
	}
	values = append(values, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM="+termProgram)
	return values
}

func validProgramEnvironment(environment map[string]string) bool {
	return validateProgramEnvironment(environment) == nil
}

func validateProgramEnvironment(environment map[string]string) error {
	if len(environment) > maxProgramEnvironmentEntries {
		return &EnvironmentValidationError{reason: "count", actual: len(environment), limit: maxProgramEnvironmentEntries}
	}
	total := 0
	for name, value := range environment {
		if !programEnvironmentNamePattern.MatchString(name) {
			return &EnvironmentValidationError{reason: "name"}
		}
		if len(value) > maxProgramEnvironmentValueBytes {
			return &EnvironmentValidationError{reason: "value-size", limit: maxProgramEnvironmentValueBytes}
		}
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return &EnvironmentValidationError{reason: "control"}
		}
		total += len(name) + len(value) + 2 // 包含等号和结束符。
	}
	if total > maxProgramEnvironmentBytes {
		return &EnvironmentValidationError{reason: "total", actual: total, limit: maxProgramEnvironmentBytes}
	}
	return nil
}

func cloneProgramEnvironment(environment map[string]string) map[string]string {
	if len(environment) == 0 {
		return nil
	}
	copy := make(map[string]string, len(environment))
	for name, value := range environment {
		copy[name] = value
	}
	return copy
}

func mergeProgramEnvironment(base []string, overrides map[string]string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = value
		}
	}
	for name, value := range overrides {
		values[name] = value
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result
}

// GUI 启动时可能没有 locale；在最终子环境中补齐文本编码，避免复制工具按旧编码读 UTF-8。
// 显式配置优先，不写父进程环境，也不把 POSIX locale 注入原生 Windows 终端。
func terminalProgramEnvironment(goos string, base []string, overrides map[string]string) []string {
	values := mergeProgramEnvironment(base, overrides)
	var fallback string
	switch goos {
	case "darwin":
		fallback = "UTF-8"
	case "linux":
		fallback = "C.UTF-8"
	default:
		return values
	}
	for _, entry := range values {
		name, value, _ := strings.Cut(entry, "=")
		if value != "" && (name == "LC_ALL" || name == "LC_CTYPE" || name == "LANG") {
			return values
		}
	}
	return mergeProgramEnvironment(values, map[string]string{"LC_CTYPE": fallback})
}
