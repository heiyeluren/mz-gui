package nativee2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func WriteReport(path string, report Report) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: 报告路径必须是绝对路径", ErrInvalidConfig)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("编码原生 E2E 报告: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建报告目录: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入原生 E2E 报告: %w", err)
	}
	return nil
}
