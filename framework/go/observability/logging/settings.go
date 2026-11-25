package logging

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Settings 是 Framework 配置文档中 [framework.logging] 的可解析字段。
// Directory 为空时回落到运行路径 LogsDir，不得静默改写到 stderr。
type Settings struct {
	Directory    string
	Level        string
	MaxFileBytes int64
}

type loggingDocument struct {
	Framework struct {
		Logging struct {
			Directory    string `toml:"directory"`
			Level        string `toml:"level"`
			MaxFileBytes int64  `toml:"max_file_bytes"`
		} `toml:"logging"`
	} `toml:"framework"`
}

func ParseSettings(document []byte) (Settings, error) {
	if len(strings.TrimSpace(string(document))) == 0 {
		return Settings{}, nil
	}
	var parsed loggingDocument
	if err := toml.Unmarshal(document, &parsed); err != nil {
		return Settings{}, fmt.Errorf("解析 framework.logging: %w", err)
	}
	return Settings{
		Directory:    strings.TrimSpace(parsed.Framework.Logging.Directory),
		Level:        strings.TrimSpace(parsed.Framework.Logging.Level),
		MaxFileBytes: parsed.Framework.Logging.MaxFileBytes,
	}, nil
}

func ResolveDirectory(settings Settings, defaultDirectory string) (string, error) {
	directory := strings.TrimSpace(settings.Directory)
	if directory == "" {
		directory = strings.TrimSpace(defaultDirectory)
	}
	if directory == "" {
		return "", fmt.Errorf("日志目录不能为空")
	}
	cleaned := filepath.Clean(directory)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("日志目录必须是绝对路径")
	}
	return cleaned, nil
}

func ApplySettings(options Options, settings Settings, defaultDirectory string) (Options, error) {
	directory, err := ResolveDirectory(settings, firstNonEmpty(options.Directory, defaultDirectory))
	if err != nil {
		return Options{}, err
	}
	options.Directory = directory
	if settings.Level != "" {
		options.Level = settings.Level
	}
	if settings.MaxFileBytes > 0 {
		options.MaxBytes = settings.MaxFileBytes
	}
	return options, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
