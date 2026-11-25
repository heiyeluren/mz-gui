package diagnostics

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrInvalidBundle = errors.New("诊断包配置无效")

type Entry struct {
	ArchiveName string
	Data        []byte
}

type File struct {
	ArchiveName string
	Path        string
	TailBytes   int64
}

type Options struct {
	OutputDirectory string
	FileName        string
	CreatedAt       time.Time
	MaxFiles        int
	MaxFileBytes    int64
	MaxTotalBytes   int64
	Entries         []Entry
	Files           []File
}

type Result struct {
	Path      string
	FileName  string
	SizeBytes int64
	SHA256    string
	CreatedAt time.Time
}

type archiveEntry struct {
	name string
	data []byte
}

// Create 将受限文本条目生成私有 ZIP，任何来源路径都不会写入归档元数据。
func Create(ctx context.Context, options Options) (Result, error) {
	if err := validateOptions(ctx, options); err != nil {
		return Result{}, err
	}
	if err := ensurePrivateDirectory(options.OutputDirectory); err != nil {
		return Result{}, err
	}
	entries, err := loadEntries(ctx, options)
	if err != nil {
		return Result{}, err
	}
	target := filepath.Join(options.OutputDirectory, options.FileName)
	if _, err := os.Lstat(target); err == nil || !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("%w: 诊断包目标已存在或不可检查", ErrInvalidBundle)
	}
	temporary, err := os.CreateTemp(options.OutputDirectory, ".mz-gui-diagnostics-*.tmp")
	if err != nil {
		return Result{}, fmt.Errorf("创建诊断包临时文件: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return Result{}, fmt.Errorf("设置诊断包权限: %w", err)
	}
	writer := zip.NewWriter(temporary)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			_ = writer.Close()
			return Result{}, err
		}
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetModTime(options.CreatedAt.UTC())
		header.SetMode(0o600)
		stream, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return Result{}, fmt.Errorf("创建诊断包条目: %w", err)
		}
		if _, err := stream.Write(entry.data); err != nil {
			_ = writer.Close()
			return Result{}, fmt.Errorf("写入诊断包条目: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return Result{}, fmt.Errorf("完成诊断包压缩: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return Result{}, fmt.Errorf("同步诊断包: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Result{}, fmt.Errorf("关闭诊断包: %w", err)
	}
	// 同目录硬链接以原子且不覆盖的方式发布已同步文件，避免同名竞态覆盖。
	if err := os.Link(temporaryPath, target); err != nil {
		return Result{}, fmt.Errorf("提交诊断包: %w", err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		_ = os.Remove(target)
		return Result{}, fmt.Errorf("固定诊断包权限: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		_ = os.Remove(target)
		return Result{}, fmt.Errorf("清理诊断包临时文件: %w", err)
	}
	committed = true
	return inspectResult(target, options.CreatedAt.UTC())
}

func validateOptions(ctx context.Context, options Options) error {
	if ctx == nil || ctx.Err() != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrInvalidBundle
	}
	cleanName := filepath.Base(strings.TrimSpace(options.FileName))
	if options.OutputDirectory == "" || !filepath.IsAbs(options.OutputDirectory) || cleanName != options.FileName ||
		!strings.HasSuffix(strings.ToLower(cleanName), ".zip") || options.CreatedAt.IsZero() || options.MaxFiles < 1 ||
		options.MaxFileBytes < 1 || options.MaxTotalBytes < 1 || len(options.Entries)+len(options.Files) < 1 ||
		len(options.Entries)+len(options.Files) > options.MaxFiles {
		return ErrInvalidBundle
	}
	return nil
}

func ensurePrivateDirectory(directory string) error {
	if info, err := os.Lstat(directory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: 诊断包目录不安全", ErrInvalidBundle)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("创建诊断包目录: %w", err)
		}
	} else {
		return fmt.Errorf("检查诊断包目录: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("设置诊断包目录权限: %w", err)
	}
	return nil
}

func loadEntries(ctx context.Context, options Options) ([]archiveEntry, error) {
	values := make([]archiveEntry, 0, len(options.Entries)+len(options.Files))
	seen := make(map[string]struct{}, cap(values))
	total := int64(0)
	appendEntry := func(name string, data []byte) error {
		name, err := validateArchiveName(name)
		if err != nil {
			return err
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: 诊断包条目重复", ErrInvalidBundle)
		}
		clean := Redact(data)
		if int64(len(clean)) > options.MaxFileBytes || total+int64(len(clean)) > options.MaxTotalBytes {
			return fmt.Errorf("%w: 诊断包内容超限", ErrInvalidBundle)
		}
		seen[name] = struct{}{}
		total += int64(len(clean))
		values = append(values, archiveEntry{name: name, data: clean})
		return nil
	}
	for _, entry := range options.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := appendEntry(entry.ArchiveName, entry.Data); err != nil {
			return nil, err
		}
	}
	for _, source := range options.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		limit := source.TailBytes
		if limit == 0 {
			limit = options.MaxFileBytes
		}
		if limit < 1 || limit > options.MaxFileBytes {
			return nil, fmt.Errorf("%w: 来源文件读取上限无效", ErrInvalidBundle)
		}
		data, err := readRegularFileTail(source.Path, limit)
		if err != nil {
			return nil, err
		}
		if err := appendEntry(source.ArchiveName, data); err != nil {
			return nil, err
		}
	}
	sort.Slice(values, func(left, right int) bool { return values[left].name < values[right].name })
	return values, nil
}

func validateArchiveName(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	clean := path.Clean(value)
	if value == "" || clean != value || clean == "." || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") {
		return "", fmt.Errorf("%w: ZIP 条目名称无效", ErrInvalidBundle)
	}
	return clean, nil
}

func readRegularFileTail(filePath string, limit int64) ([]byte, error) {
	filePath = filepath.Clean(strings.TrimSpace(filePath))
	if filePath == "" || !filepath.IsAbs(filePath) {
		return nil, fmt.Errorf("%w: 来源文件路径无效", ErrInvalidBundle)
	}
	info, err := os.Lstat(filePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: 来源文件不安全", ErrInvalidBundle)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开诊断来源文件: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("%w: 来源文件在打开时发生变化", ErrInvalidBundle)
	}
	start := info.Size() - limit
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("定位诊断来源文件: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return nil, fmt.Errorf("读取诊断来源文件: %w", err)
	}
	return data, nil
}

func inspectResult(target string, createdAt time.Time) (Result, error) {
	data, err := os.ReadFile(target)
	if err != nil {
		return Result{}, fmt.Errorf("校验诊断包: %w", err)
	}
	digest := sha256.Sum256(data)
	return Result{Path: target, FileName: filepath.Base(target), SizeBytes: int64(len(data)),
		SHA256: hex.EncodeToString(digest[:]), CreatedAt: createdAt}, nil
}
