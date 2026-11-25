package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	RecoverActionNone           = "none"
	RecoverActionRestored       = "restored"
	RecoverActionRepairRequired = "repair_required"
)

type RecoverOptions struct {
	TargetPath     string
	BackupPath     string
	QuarantinePath string
	MaxBytes       int64
}

type RecoverResult struct {
	Action     string
	Target     FileSnapshot
	Quarantine string
}

// RecoverIfCorrupt 在 quick_check 失败时隔离损坏库及其 WAL/SHM，并尝试从已校验备份恢复。
func RecoverIfCorrupt(ctx context.Context, options RecoverOptions) (RecoverResult, error) {
	if ctx == nil {
		return RecoverResult{}, fmt.Errorf("SQLite recover context 不能为空")
	}
	target, backup, quarantine, maxBytes, err := normalizeRecoverOptions(options)
	if err != nil {
		return RecoverResult{}, err
	}
	if _, err := os.Lstat(target); err == nil {
		if snapshot, verifyErr := VerifyFile(ctx, target, maxBytes); verifyErr == nil {
			return RecoverResult{Action: RecoverActionNone, Target: snapshot}, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return RecoverResult{}, fmt.Errorf("检查 SQLite recover 目标: %w", err)
	}

	if _, err := os.Lstat(backup); err == nil {
		if _, verifyErr := VerifyFile(ctx, backup, maxBytes); verifyErr == nil {
			if err := relocateSQLiteSidecars(target, quarantine); err != nil {
				return RecoverResult{}, err
			}
			snapshot, restoreErr := Restore(ctx, RestoreOptions{
				BackupPath: backup, TargetPath: target, QuarantinePath: quarantine, MaxBytes: maxBytes,
			})
			if restoreErr != nil {
				return RecoverResult{Action: RecoverActionRepairRequired, Quarantine: quarantine}, nil
			}
			return RecoverResult{Action: RecoverActionRestored, Target: snapshot, Quarantine: quarantine}, nil
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return RecoverResult{}, fmt.Errorf("检查 SQLite recover 备份: %w", err)
	}

	if err := isolateDamagedDatabase(target, quarantine); err != nil {
		return RecoverResult{}, err
	}
	return RecoverResult{Action: RecoverActionRepairRequired, Quarantine: quarantine}, nil
}

func normalizeRecoverOptions(options RecoverOptions) (string, string, string, int64, error) {
	target, maxBytes, err := normalizeSQLiteFilePathAllowMissing(options.TargetPath, options.MaxBytes)
	if err != nil {
		return "", "", "", 0, err
	}
	backup, maxBytes, err := normalizeSQLiteFilePathAllowMissing(options.BackupPath, maxBytes)
	if err != nil {
		return "", "", "", 0, err
	}
	quarantine, maxBytes, err := normalizeSQLiteFilePathAllowMissing(options.QuarantinePath, maxBytes)
	if err != nil {
		return "", "", "", 0, err
	}
	if samePath(target, backup) || samePath(target, quarantine) || samePath(backup, quarantine) {
		return "", "", "", 0, fmt.Errorf("SQLite recover 路径不能重复")
	}
	return target, backup, quarantine, maxBytes, nil
}

func isolateDamagedDatabase(target, quarantine string) error {
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("检查损坏 SQLite: %w", err)
	}
	if err := ensureParentPrivate(quarantine); err != nil {
		return err
	}
	if err := relocateSQLiteSidecars(target, quarantine); err != nil {
		return err
	}
	if _, err := os.Lstat(quarantine); err == nil {
		return fmt.Errorf("SQLite recover quarantine 已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("检查 SQLite recover quarantine: %w", err)
	}
	if err := os.Rename(target, quarantine); err != nil {
		return fmt.Errorf("隔离损坏 SQLite: %w", err)
	}
	return nil
}

func relocateSQLiteSidecars(sourceMain, destMain string) error {
	if err := os.MkdirAll(filepath.Dir(destMain), 0o700); err != nil {
		return fmt.Errorf("创建 SQLite sidecar 目录: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		source := sourceMain + suffix
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("检查 SQLite sidecar: %w", err)
		}
		dest := destMain + suffix
		if _, err := os.Lstat(dest); err == nil {
			if err := os.Remove(dest); err != nil {
				return fmt.Errorf("清理 SQLite sidecar 目标: %w", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("检查 SQLite sidecar 目标: %w", err)
		}
		if err := os.Rename(source, dest); err != nil {
			return fmt.Errorf("隔离 SQLite sidecar: %w", err)
		}
	}
	return nil
}
