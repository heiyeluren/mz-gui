package terminal

import (
	"errors"
	"runtime"
	"strings"
	"unicode"
)

const (
	MaximumPastePathCount = 64
	MaximumPastePathBytes = 64 * 1024
)

type ShellDialect string

const (
	ShellPOSIX      ShellDialect = "posix"
	ShellPowerShell ShellDialect = "powershell"
)

var (
	ErrNoPastePaths       = errors.New("没有可粘贴的文件路径")
	ErrTooManyPastePaths  = errors.New("文件路径数量超过上限")
	ErrEmptyPastePath     = errors.New("文件路径为空")
	ErrPastePathsTooLarge = errors.New("文件路径总大小超过上限")
	ErrInvalidShell       = errors.New("终端 Shell 类型无效")
	ErrUnsupportedPath    = errors.New("当前终端不支持该文件路径")
)

// EncodeFilePaths 把原生文件路径编码成可安全写入终端命令行的参数文本。
func EncodeFilePaths(paths []string, dialect ShellDialect) (string, error) {
	if len(paths) == 0 {
		return "", ErrNoPastePaths
	}
	if len(paths) > MaximumPastePathCount {
		return "", ErrTooManyPastePaths
	}
	if dialect != ShellPOSIX && dialect != ShellPowerShell {
		return "", ErrInvalidShell
	}
	total := 0
	encoded := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			return "", ErrEmptyPastePath
		}
		if len(path) > MaximumPastePathBytes-total {
			return "", ErrPastePathsTooLarge
		}
		total += len(path)
		value, err := encodeFilePath(path, dialect)
		if err != nil {
			return "", err
		}
		encoded = append(encoded, value)
	}
	return strings.Join(encoded, " ") + " ", nil
}

func EncodeFilePathsForCurrentPlatform(paths []string) (string, error) {
	if runtime.GOOS == "windows" {
		return EncodeFilePaths(paths, ShellPowerShell)
	}
	return EncodeFilePaths(paths, ShellPOSIX)
}

func encodeFilePath(path string, dialect ShellDialect) (string, error) {
	if dialect == ShellPowerShell {
		for _, value := range path {
			if unicode.IsControl(value) {
				return "", ErrUnsupportedPath
			}
		}
		return "'" + strings.ReplaceAll(path, "'", "''") + "'", nil
	}
	for _, value := range path {
		if unicode.IsControl(value) {
			return encodePOSIXControlPath(path), nil
		}
	}
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'", nil
}

func encodePOSIXControlPath(path string) string {
	var result strings.Builder
	result.WriteString("$'")
	for _, value := range path {
		switch value {
		case '\x00':
			result.WriteString(`\x00`)
		case '\t':
			result.WriteString(`\t`)
		case '\n':
			result.WriteString(`\n`)
		case '\r':
			result.WriteString(`\r`)
		case '\x1b':
			result.WriteString(`\x1b`)
		case '\'':
			result.WriteString(`\'`)
		case '\\':
			result.WriteString(`\\`)
		default:
			if unicode.IsControl(value) {
				writePOSIXControlEscape(&result, value)
			} else {
				result.WriteRune(value)
			}
		}
	}
	result.WriteByte('\'')
	return result.String()
}

func writePOSIXControlEscape(result *strings.Builder, value rune) {
	const hex = "0123456789abcdef"
	width := 8
	prefix := `\U`
	if value <= 0xff {
		width, prefix = 2, `\x`
	} else if value <= 0xffff {
		width, prefix = 4, `\u`
	}
	result.WriteString(prefix)
	for shift := (width - 1) * 4; shift >= 0; shift -= 4 {
		result.WriteByte(hex[(value>>shift)&0xf])
	}
}
