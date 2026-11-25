package diagnostics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type PrivacyClass string

const (
	PrivacyPublic    PrivacyClass = "public"
	PrivacyInternal  PrivacyClass = "internal"
	PrivacySensitive PrivacyClass = "sensitive"
	PrivacySecret    PrivacyClass = "secret"
)

type CrashSpoolOptions struct {
	Directory      string
	MaxRecords     int
	MaxRecordBytes int64
	Now            func() time.Time
}

type CrashSpool struct {
	options CrashSpoolOptions
}

type Breadcrumb struct {
	Name         string            `json:"name"`
	PrivacyClass PrivacyClass      `json:"privacy_class"`
	Attributes   map[string]string `json:"attributes,omitempty"`
}

type CrashRecord struct {
	SchemaVersion  string       `json:"schema_version"`
	Fingerprint    string       `json:"fingerprint"`
	ProcessRole    string       `json:"process_role"`
	Platform       string       `json:"platform,omitempty"`
	Component      string       `json:"component"`
	ErrorClass     string       `json:"error_class"`
	ReasonCode     string       `json:"reason_code"`
	BuildID        string       `json:"build_id,omitempty"`
	SystemSummary  string       `json:"system_summary,omitempty"`
	WebViewSummary string       `json:"webview_summary,omitempty"`
	StackSummary   string       `json:"stack_summary,omitempty"`
	Breadcrumbs    []Breadcrumb `json:"breadcrumbs,omitempty"`
	OccurredAt     time.Time    `json:"occurred_at"`
}

type CrashSpoolResult struct {
	Path        string    `json:"path"`
	FileName    string    `json:"file_name"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
	Fingerprint string    `json:"fingerprint"`
}

var (
	ErrInvalidCrashSpool = errors.New("崩溃 spool 配置无效")
	crashTokenPattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{1,127}$`)
)

func NewCrashSpool(options CrashSpoolOptions) (*CrashSpool, error) {
	if options.Directory == "" || !filepath.IsAbs(options.Directory) || options.MaxRecords < 1 || options.MaxRecords > 1000 ||
		options.MaxRecordBytes < 512 || options.MaxRecordBytes > 1<<20 {
		return nil, ErrInvalidCrashSpool
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if err := ensurePrivateDirectory(options.Directory); err != nil {
		return nil, err
	}
	return &CrashSpool{options: options}, nil
}

func (spool *CrashSpool) Write(ctx context.Context, record CrashRecord) (CrashSpoolResult, error) {
	if ctx == nil || spool == nil {
		return CrashSpoolResult{}, ErrInvalidCrashSpool
	}
	if err := ctx.Err(); err != nil {
		return CrashSpoolResult{}, err
	}
	normalized, err := normalizeCrashRecord(record)
	if err != nil {
		return CrashSpoolResult{}, err
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return CrashSpoolResult{}, fmt.Errorf("序列化崩溃记录: %w", err)
	}
	data = Redact(data)
	if int64(len(data)) > spool.options.MaxRecordBytes {
		return CrashSpoolResult{}, fmt.Errorf("%w: 崩溃记录超过上限", ErrInvalidCrashSpool)
	}
	name := fmt.Sprintf("%s-%s.json", normalized.OccurredAt.UTC().Format("20060102T150405.000000000Z"), safeFileToken(normalized.Fingerprint))
	target := filepath.Join(spool.options.Directory, name)
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return CrashSpoolResult{}, fmt.Errorf("创建崩溃记录: %w", err)
	}
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(target)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return CrashSpoolResult{}, fmt.Errorf("写入崩溃记录: %w", err)
	}
	if err := file.Sync(); err != nil {
		return CrashSpoolResult{}, fmt.Errorf("同步崩溃记录: %w", err)
	}
	if err := file.Close(); err != nil {
		return CrashSpoolResult{}, fmt.Errorf("关闭崩溃记录: %w", err)
	}
	committed = true
	if err := spool.enforceCapacity(); err != nil {
		return CrashSpoolResult{}, err
	}
	digest := sha256.Sum256(data)
	return CrashSpoolResult{
		Path: target, FileName: name, SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:]),
		CreatedAt: spool.options.Now().UTC(), Fingerprint: normalized.Fingerprint,
	}, nil
}

func normalizeCrashRecord(record CrashRecord) (CrashRecord, error) {
	record.SchemaVersion = "mzgui.crash.v1"
	record.Fingerprint = strings.TrimSpace(record.Fingerprint)
	record.ProcessRole = strings.TrimSpace(record.ProcessRole)
	record.Platform = strings.TrimSpace(record.Platform)
	record.Component = strings.TrimSpace(record.Component)
	record.ErrorClass = strings.TrimSpace(record.ErrorClass)
	record.ReasonCode = strings.TrimSpace(record.ReasonCode)
	if record.OccurredAt.IsZero() || !crashTokenPattern.MatchString(record.Fingerprint) || !crashTokenPattern.MatchString(record.ProcessRole) ||
		!crashTokenPattern.MatchString(record.Component) || !crashTokenPattern.MatchString(record.ErrorClass) || !crashTokenPattern.MatchString(record.ReasonCode) ||
		(record.Platform != "" && !crashTokenPattern.MatchString(record.Platform)) {
		return CrashRecord{}, ErrInvalidCrashSpool
	}
	record.OccurredAt = record.OccurredAt.UTC()
	record.StackSummary = string(Redact([]byte(strings.TrimSpace(record.StackSummary))))
	record.SystemSummary = string(Redact([]byte(strings.TrimSpace(record.SystemSummary))))
	record.WebViewSummary = string(Redact([]byte(strings.TrimSpace(record.WebViewSummary))))
	record.Breadcrumbs = normalizeBreadcrumbs(record.Breadcrumbs)
	return record, nil
}

func normalizeBreadcrumbs(values []Breadcrumb) []Breadcrumb {
	if len(values) == 0 {
		return nil
	}
	result := make([]Breadcrumb, 0, min(len(values), 32))
	for _, value := range values {
		name := strings.TrimSpace(value.Name)
		if !crashTokenPattern.MatchString(name) || !value.PrivacyClass.validManifestPrivacy() || value.PrivacyClass == PrivacySecret {
			continue
		}
		result = append(result, Breadcrumb{Name: name, PrivacyClass: value.PrivacyClass, Attributes: redactStringMap(value.Attributes)})
		if len(result) >= 32 {
			break
		}
	}
	return result
}

func redactStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		cleanKey := strings.ToLower(strings.TrimSpace(key))
		if cleanKey == "" {
			continue
		}
		result[cleanKey] = string(Redact([]byte(values[key])))
	}
	return result
}

func (spool *CrashSpool) enforceCapacity() error {
	files, err := filepath.Glob(filepath.Join(spool.options.Directory, "*.json"))
	if err != nil {
		return fmt.Errorf("扫描崩溃记录: %w", err)
	}
	sort.Strings(files)
	for len(files) > spool.options.MaxRecords {
		if err := os.Remove(files[0]); err != nil {
			return fmt.Errorf("清理崩溃记录: %w", err)
		}
		files = files[1:]
	}
	return nil
}

func safeFileToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			builder.WriteRune(char)
		}
	}
	if builder.Len() == 0 {
		return "crash"
	}
	return builder.String()
}
