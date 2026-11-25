package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/observability/sampleauth"
)

var ErrInvalidCrashUpload = errors.New("崩溃上报配置无效")

type CrashTransport interface {
	Upload(ctx context.Context, record CrashRecord, payload []byte) error
}

type CrashUploadOptions struct {
	Directory      string
	Capabilities   []string
	Policy         sampleauth.Policy
	Transport      CrashTransport
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	MaxAge         time.Duration
	Now            func() time.Time
}

type CrashUploadReport struct {
	Uploaded int `json:"uploaded"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
	Expired  int `json:"expired"`
}

type crashUploadState struct {
	SchemaVersion string    `json:"schema_version"`
	FileName      string    `json:"file_name"`
	Fingerprint   string    `json:"fingerprint"`
	Attempts      int       `json:"attempts"`
	NextRetryAt   time.Time `json:"next_retry_at"`
	LastError     string    `json:"last_error,omitempty"`
	Status        string    `json:"status"`
}

// FlushCrashUploads 仅在具备 crashes 上报 capability 时发送采样记录，失败走退避，超龄或超过重试上限则清理。
func FlushCrashUploads(ctx context.Context, options CrashUploadOptions) (CrashUploadReport, error) {
	if err := validateCrashUpload(ctx, options); err != nil {
		return CrashUploadReport{}, err
	}
	if err := sampleauth.AuthorizeUpload(sampleauth.UploadCrashes, options.Capabilities); err != nil {
		return CrashUploadReport{}, err
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	files, err := filepath.Glob(filepath.Join(options.Directory, "*.json"))
	if err != nil {
		return CrashUploadReport{}, fmt.Errorf("扫描崩溃 spool: %w", err)
	}
	sort.Strings(files)
	var report CrashUploadReport
	for _, file := range files {
		kind, err := flushOneCrash(ctx, options, file, now().UTC())
		if err != nil {
			return report, err
		}
		switch kind {
		case "uploaded":
			report.Uploaded++
		case "skipped":
			report.Skipped++
		case "failed":
			report.Failed++
		case "expired":
			report.Expired++
		}
	}
	return report, nil
}

func validateCrashUpload(ctx context.Context, options CrashUploadOptions) error {
	if ctx == nil || options.Transport == nil || options.Directory == "" || !filepath.IsAbs(options.Directory) ||
		options.MaxAttempts < 1 || options.MaxAttempts > 32 || options.InitialBackoff <= 0 ||
		options.MaxBackoff < options.InitialBackoff || options.MaxAge < options.InitialBackoff {
		return ErrInvalidCrashUpload
	}
	if err := options.Policy.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func flushOneCrash(ctx context.Context, options CrashUploadOptions, path string, now time.Time) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var record CrashRecord
	if err := json.Unmarshal(data, &record); err != nil {
		_ = os.Remove(path)
		_ = os.Remove(uploadStatePath(path))
		return "expired", nil
	}
	if record.OccurredAt.IsZero() || now.Sub(record.OccurredAt.UTC()) > options.MaxAge {
		_ = os.Remove(path)
		_ = os.Remove(uploadStatePath(path))
		return "expired", nil
	}
	if !options.Policy.Sampled(record.Fingerprint) {
		return "skipped", nil
	}
	state, _ := readUploadState(path)
	if !state.NextRetryAt.IsZero() && now.Before(state.NextRetryAt) {
		return "skipped", nil
	}
	if err := options.Transport.Upload(ctx, record, data); err == nil {
		_ = os.Remove(path)
		_ = os.Remove(uploadStatePath(path))
		return "uploaded", nil
	} else {
		state.SchemaVersion = "mzgui.crash.upload.v1"
		state.FileName = filepath.Base(path)
		state.Fingerprint = record.Fingerprint
		state.Attempts++
		state.LastError = "upload_failed"
		state.Status = "pending"
		if state.Attempts >= options.MaxAttempts {
			_ = os.Remove(path)
			_ = os.Remove(uploadStatePath(path))
			return "expired", nil
		}
		state.NextRetryAt = now.Add(uploadBackoff(options, state.Attempts))
		if err := writeUploadState(path, state); err != nil {
			return "", err
		}
		return "failed", nil
	}
}

func uploadBackoff(options CrashUploadOptions, attempts int) time.Duration {
	backoff := options.InitialBackoff
	for i := 1; i < attempts; i++ {
		if backoff >= options.MaxBackoff/2 {
			return options.MaxBackoff
		}
		backoff *= 2
	}
	if backoff > options.MaxBackoff {
		return options.MaxBackoff
	}
	return backoff
}

func uploadStatePath(recordPath string) string {
	return recordPath + ".state"
}

func readUploadState(recordPath string) (crashUploadState, error) {
	data, err := os.ReadFile(uploadStatePath(recordPath))
	if err != nil {
		return crashUploadState{}, err
	}
	var state crashUploadState
	if err := json.Unmarshal(data, &state); err != nil {
		return crashUploadState{}, err
	}
	return state, nil
}

func writeUploadState(recordPath string, state crashUploadState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(uploadStatePath(recordPath), data, 0o600)
}
