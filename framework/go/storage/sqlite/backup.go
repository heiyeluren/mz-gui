package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type BackupOptions struct {
	SourcePath      string
	DestinationPath string
	MaxBytes        int64
}

type RestoreOptions struct {
	BackupPath     string
	TargetPath     string
	QuarantinePath string
	MaxBytes       int64
}

type FileSnapshot struct {
	Path   string
	Size   int64
	SHA256 string
}

const defaultSQLiteBackupMaxBytes int64 = 64 << 30

// Backup 创建 SQLite 一致性备份，并对备份文件执行 quick_check 与 sha256 记录。
func Backup(ctx context.Context, database *sql.DB, options BackupOptions) (FileSnapshot, error) {
	if ctx == nil || database == nil {
		return FileSnapshot{}, fmt.Errorf("SQLite backup 参数无效")
	}
	source, destination, maxBytes, err := normalizeBackupOptions(options.SourcePath, options.DestinationPath, options.MaxBytes)
	if err != nil {
		return FileSnapshot{}, err
	}
	if err := ensureParentPrivate(destination); err != nil {
		return FileSnapshot{}, err
	}
	if _, err := os.Lstat(destination); err == nil {
		return FileSnapshot{}, fmt.Errorf("SQLite backup 目标已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileSnapshot{}, fmt.Errorf("检查 SQLite backup 目标: %w", err)
	}
	if _, err := database.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return FileSnapshot{}, fmt.Errorf("SQLite backup checkpoint: %w", err)
	}
	statement := "VACUUM main INTO " + quoteSQLString(filepath.ToSlash(destination))
	if _, err := database.ExecContext(ctx, statement); err != nil {
		return FileSnapshot{}, fmt.Errorf("SQLite backup vacuum: %w", err)
	}
	if err := os.Chmod(destination, 0o600); err != nil {
		return FileSnapshot{}, fmt.Errorf("设置 SQLite backup 权限: %w", err)
	}
	snapshot, err := VerifyFile(ctx, destination, maxBytes)
	if err != nil {
		return FileSnapshot{}, err
	}
	if source != "" && samePath(source, destination) {
		return FileSnapshot{}, fmt.Errorf("SQLite backup 源和目标不能相同")
	}
	return snapshot, nil
}

// Restore 验证备份文件后恢复到目标路径，已有目标会先移动到 quarantine。
func Restore(ctx context.Context, options RestoreOptions) (FileSnapshot, error) {
	if ctx == nil {
		return FileSnapshot{}, fmt.Errorf("SQLite restore context 不能为空")
	}
	backup, target, quarantine, maxBytes, err := normalizeRestoreOptions(options)
	if err != nil {
		return FileSnapshot{}, err
	}
	if _, err := VerifyFile(ctx, backup, maxBytes); err != nil {
		return FileSnapshot{}, fmt.Errorf("SQLite restore 备份无效: %w", err)
	}
	if err := ensureParentPrivate(target); err != nil {
		return FileSnapshot{}, err
	}
	if err := ensureParentPrivate(quarantine); err != nil {
		return FileSnapshot{}, err
	}
	staging := target + ".restore-staging"
	if _, err := os.Lstat(staging); err == nil {
		return FileSnapshot{}, fmt.Errorf("SQLite restore staging 已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileSnapshot{}, fmt.Errorf("检查 SQLite restore staging: %w", err)
	}
	hadTarget := false
	if _, err := os.Lstat(target); err == nil {
		hadTarget = true
		if _, err := os.Lstat(quarantine); err == nil {
			return FileSnapshot{}, fmt.Errorf("SQLite restore quarantine 已存在")
		} else if !errors.Is(err, os.ErrNotExist) {
			return FileSnapshot{}, fmt.Errorf("检查 SQLite restore quarantine: %w", err)
		}
		if err := os.Rename(target, quarantine); err != nil {
			return FileSnapshot{}, fmt.Errorf("隔离旧 SQLite 数据库: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileSnapshot{}, fmt.Errorf("检查 SQLite restore 目标: %w", err)
	}
	if err := copyPrivateFile(backup, staging, maxBytes); err != nil {
		if hadTarget {
			_ = os.Rename(quarantine, target)
		}
		return FileSnapshot{}, err
	}
	snapshot, err := VerifyFile(ctx, staging, maxBytes)
	if err != nil {
		if hadTarget {
			_ = os.Rename(quarantine, target)
		}
		return FileSnapshot{}, err
	}
	if err := os.Rename(staging, target); err != nil {
		if hadTarget {
			_ = os.Rename(quarantine, target)
		}
		return FileSnapshot{}, fmt.Errorf("提交 SQLite restore 目标: %w", err)
	}
	snapshot.Path = target
	return snapshot, nil
}

// VerifyFile 对 SQLite 文件执行容量、sha256 和 PRAGMA quick_check 校验。
func VerifyFile(ctx context.Context, path string, maxBytes int64) (FileSnapshot, error) {
	if ctx == nil {
		return FileSnapshot{}, fmt.Errorf("SQLite verify context 不能为空")
	}
	canonical, maxBytes, err := normalizeSQLiteFilePath(path, maxBytes)
	if err != nil {
		return FileSnapshot{}, err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return FileSnapshot{}, fmt.Errorf("读取 SQLite 文件: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBytes {
		return FileSnapshot{}, fmt.Errorf("SQLite 文件大小无效")
	}
	sum, err := fileSHA256(canonical)
	if err != nil {
		return FileSnapshot{}, err
	}
	dsn := "file:" + filepath.ToSlash(canonical) + "?mode=ro&_pragma=query_only(1)&_pragma=foreign_keys(1)"
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return FileSnapshot{}, fmt.Errorf("打开 SQLite verify: %w", err)
	}
	defer database.Close()
	var result string
	if err := database.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return FileSnapshot{}, fmt.Errorf("SQLite quick_check 失败: %w", err)
	}
	if result != "ok" {
		return FileSnapshot{}, fmt.Errorf("SQLite quick_check 非 ok: %s", result)
	}
	return FileSnapshot{Path: canonical, Size: info.Size(), SHA256: "sha256:" + sum}, nil
}

func normalizeBackupOptions(sourcePath, destinationPath string, maxBytes int64) (string, string, int64, error) {
	source, _, err := normalizeSQLiteFilePathAllowMissing(sourcePath, maxBytes)
	if err != nil && sourcePath != "" {
		return "", "", 0, err
	}
	destination, maxBytes, err := normalizeSQLiteFilePathAllowMissing(destinationPath, maxBytes)
	if err != nil {
		return "", "", 0, err
	}
	if source != "" && samePath(source, destination) {
		return "", "", 0, fmt.Errorf("SQLite backup 源和目标不能相同")
	}
	return source, destination, maxBytes, nil
}

func normalizeRestoreOptions(options RestoreOptions) (string, string, string, int64, error) {
	backup, maxBytes, err := normalizeSQLiteFilePath(options.BackupPath, options.MaxBytes)
	if err != nil {
		return "", "", "", 0, err
	}
	target, maxBytes, err := normalizeSQLiteFilePathAllowMissing(options.TargetPath, maxBytes)
	if err != nil {
		return "", "", "", 0, err
	}
	quarantine, maxBytes, err := normalizeSQLiteFilePathAllowMissing(options.QuarantinePath, maxBytes)
	if err != nil {
		return "", "", "", 0, err
	}
	if samePath(backup, target) || samePath(target, quarantine) || samePath(backup, quarantine) {
		return "", "", "", 0, fmt.Errorf("SQLite restore 路径不能重复")
	}
	return backup, target, quarantine, maxBytes, nil
}

func normalizeSQLiteFilePath(path string, maxBytes int64) (string, int64, error) {
	canonical, maxBytes, err := normalizeSQLiteFilePathAllowMissing(path, maxBytes)
	if err != nil {
		return "", 0, err
	}
	if _, err := os.Stat(canonical); err != nil {
		return "", 0, fmt.Errorf("SQLite 文件不存在: %w", err)
	}
	return canonical, maxBytes, nil
}

func normalizeSQLiteFilePathAllowMissing(path string, maxBytes int64) (string, int64, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", 0, fmt.Errorf("SQLite 文件路径必须是绝对路径")
	}
	clean := filepath.Clean(path)
	if clean == string(filepath.Separator) || strings.HasSuffix(clean, string(filepath.Separator)) {
		return "", 0, fmt.Errorf("SQLite 文件路径无效")
	}
	if maxBytes == 0 {
		maxBytes = defaultSQLiteBackupMaxBytes
	}
	if maxBytes < 4096 || maxBytes > defaultSQLiteBackupMaxBytes {
		return "", 0, fmt.Errorf("SQLite 文件容量限制无效")
	}
	return clean, maxBytes, nil
}

func ensureParentPrivate(path string) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("创建 SQLite 目录: %w", err)
	}
	return os.Chmod(parent, 0o700)
}

func copyPrivateFile(source, target string, maxBytes int64) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("打开 SQLite restore 备份: %w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("创建 SQLite restore 目标: %w", err)
	}
	defer output.Close()
	written, err := io.Copy(output, io.LimitReader(input, maxBytes+1))
	if err != nil {
		return fmt.Errorf("复制 SQLite restore 备份: %w", err)
	}
	if written > maxBytes {
		return fmt.Errorf("SQLite restore 备份超出容量限制")
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("同步 SQLite restore 目标: %w", err)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开 SQLite 文件计算 sha256: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("计算 SQLite 文件 sha256: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func quoteSQLString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	return filepath.Clean(left) == filepath.Clean(right)
}
