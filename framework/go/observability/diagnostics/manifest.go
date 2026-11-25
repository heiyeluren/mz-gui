package diagnostics

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ManifestOptions struct {
	SchemaVersion string
	Artifact      string
	CreatedAt     time.Time
	Files         []ManifestFile
}

type ManifestFile struct {
	Name         string
	Path         string
	PrivacyClass PrivacyClass
}

type Manifest struct {
	SchemaVersion string               `json:"schema_version"`
	Artifact      string               `json:"artifact"`
	CreatedAt     time.Time            `json:"created_at"`
	Files         []ManifestFileResult `json:"files"`
}

type ManifestFileResult struct {
	Name         string       `json:"name"`
	SizeBytes    int64        `json:"size_bytes"`
	SHA256       string       `json:"sha256"`
	PrivacyClass PrivacyClass `json:"privacy_class"`
}

func BuildManifest(options ManifestOptions) (Manifest, error) {
	if strings.TrimSpace(options.SchemaVersion) == "" || strings.TrimSpace(options.Artifact) == "" || options.CreatedAt.IsZero() || len(options.Files) == 0 {
		return Manifest{}, ErrInvalidBundle
	}
	files := make([]ManifestFileResult, 0, len(options.Files))
	seen := make(map[string]struct{}, len(options.Files))
	for _, file := range options.Files {
		result, err := inspectManifestFile(file)
		if err != nil {
			return Manifest{}, err
		}
		if _, exists := seen[result.Name]; exists {
			return Manifest{}, fmt.Errorf("%w: 诊断 manifest 文件重复", ErrInvalidBundle)
		}
		seen[result.Name] = struct{}{}
		files = append(files, result)
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Name < files[right].Name })
	return Manifest{
		SchemaVersion: strings.TrimSpace(options.SchemaVersion),
		Artifact:      strings.TrimSpace(options.Artifact),
		CreatedAt:     options.CreatedAt.UTC(),
		Files:         files,
	}, nil
}

func inspectManifestFile(file ManifestFile) (ManifestFileResult, error) {
	name, err := validateArchiveName(file.Name)
	if err != nil {
		return ManifestFileResult{}, err
	}
	if !file.PrivacyClass.validManifestPrivacy() || file.PrivacyClass == PrivacySecret {
		return ManifestFileResult{}, fmt.Errorf("%w: 诊断 manifest 拒绝 secret 文件", ErrInvalidBundle)
	}
	path := filepath.Clean(strings.TrimSpace(file.Path))
	if path == "" || !filepath.IsAbs(path) {
		return ManifestFileResult{}, fmt.Errorf("%w: 诊断 manifest 文件路径无效", ErrInvalidBundle)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		if err == nil {
			err = errors.New("not regular file")
		}
		return ManifestFileResult{}, fmt.Errorf("%w: 诊断 manifest 文件不安全: %v", ErrInvalidBundle, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ManifestFileResult{}, fmt.Errorf("读取诊断 manifest 文件: %w", err)
	}
	digest := sha256.Sum256(data)
	return ManifestFileResult{Name: name, SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), PrivacyClass: file.PrivacyClass}, nil
}

func (value PrivacyClass) validManifestPrivacy() bool {
	switch value {
	case PrivacyPublic, PrivacyInternal, PrivacySensitive, PrivacySecret:
		return true
	default:
		return false
	}
}
