package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CrashMatrixRow struct {
	Fingerprint    string    `json:"fingerprint"`
	Count          int       `json:"count"`
	ProcessRole    string    `json:"process_role"`
	Platform       string    `json:"platform,omitempty"`
	ErrorClass     string    `json:"error_class"`
	LastOccurredAt time.Time `json:"last_occurred_at"`
}

func NormalizeCrashPlatform(goos, goarch string) string {
	goos = strings.TrimSpace(strings.ToLower(goos))
	goarch = strings.TrimSpace(strings.ToLower(goarch))
	switch goos {
	case "darwin":
		if goarch == "arm64" {
			return "macos-arm64"
		}
		return "macos-x86_64"
	case "windows":
		if goarch == "arm64" {
			return "windows-arm64"
		}
		return "windows-x64"
	default:
		if goos == "" || goarch == "" {
			return ""
		}
		return goos + "-" + goarch
	}
}

func BuildCrashMatrix(ctx context.Context, directory string) ([]CrashMatrixRow, error) {
	if ctx == nil || strings.TrimSpace(directory) == "" {
		return nil, ErrInvalidCrashSpool
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("扫描崩溃 spool: %w", err)
	}
	grouped := map[string]*CrashMatrixRow{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var record CrashRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("解析崩溃记录 %s: %w", filepath.Base(file), err)
		}
		key := record.Fingerprint + "\x00" + record.Platform
		row := grouped[key]
		if row == nil {
			row = &CrashMatrixRow{
				Fingerprint:    record.Fingerprint,
				ProcessRole:    record.ProcessRole,
				Platform:       record.Platform,
				ErrorClass:     record.ErrorClass,
				LastOccurredAt: record.OccurredAt,
			}
			grouped[key] = row
		}
		row.Count++
		if record.OccurredAt.After(row.LastOccurredAt) {
			row.LastOccurredAt = record.OccurredAt
		}
	}
	matrix := make([]CrashMatrixRow, 0, len(grouped))
	for _, row := range grouped {
		matrix = append(matrix, *row)
	}
	sort.Slice(matrix, func(i, j int) bool {
		if matrix[i].Count == matrix[j].Count {
			if matrix[i].Fingerprint == matrix[j].Fingerprint {
				return matrix[i].Platform < matrix[j].Platform
			}
			return matrix[i].Fingerprint < matrix[j].Fingerprint
		}
		return matrix[i].Count > matrix[j].Count
	})
	return matrix, nil
}
