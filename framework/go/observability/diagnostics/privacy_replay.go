package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// W115-G684：结构化日志/指标/trace/crash spool 隐私回读。不编造事件。诊断包见 T137。

// ReplayCrashSpool 回读本地 crash spool，二次脱敏后返回已有记录。
// 目录不存在或为空时返回空切片，不得编造崩溃事件；回读不删除、不创建、不上传。
func ReplayCrashSpool(ctx context.Context, directory string) ([]CrashRecord, error) {
	if ctx == nil {
		return nil, ErrInvalidCrashSpool
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory = filepath.Clean(strings.TrimSpace(directory))
	if directory == "" || !filepath.IsAbs(directory) {
		return nil, ErrInvalidCrashSpool
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, ErrInvalidCrashSpool
	}
	files, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	records := make([]CrashRecord, 0, len(files))
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fileInfo, statErr := os.Lstat(path)
		if statErr != nil || fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() {
			continue
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		data = Redact(data)
		var record CrashRecord
		if json.Unmarshal(data, &record) != nil {
			continue
		}
		if strings.TrimSpace(record.Fingerprint) == "" || strings.TrimSpace(record.ReasonCode) == "" {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// ReplayLogPrivacy 对已落盘 JSONL/文本做二次脱敏回读。空输入返回空，不编造日志行。
func ReplayLogPrivacy(data []byte) []byte {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return Redact(data)
}
