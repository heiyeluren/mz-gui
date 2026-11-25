// Package directorytxn 提供产品无关的受控目录替换事务。
package directorytxn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidOptions   = errors.New("目录事务参数无效")
	ErrUnsafePath       = errors.New("目录事务路径类型不安全")
	ErrStagingMissing   = errors.New("目录事务暂存目录不存在")
	ErrTargetMissing    = errors.New("目录事务目标目录不存在")
	ErrBackupConflict   = errors.New("目录事务备份目录已存在")
	ErrRollbackConflict = errors.New("目录事务回滚状态冲突")
)

var componentPattern = regexp.MustCompile(`^\.?[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Options struct {
	Root             string
	TargetDirectory  string
	StagingDirectory string
	BackupDirectory  string
}

type ReplaceRequest struct {
	Key       string
	StagingID string
	BackupID  string
}

type RemoveRequest struct {
	Key      string
	BackupID string
	// Permanent 由调用方显式选择永久删除；默认仍保留备份。
	Permanent bool
}

type RollbackReplaceRequest struct {
	// RequireTargetAbsent 用于撤销删除；存在后续新目录时拒绝覆盖。
	RequireTargetAbsent bool
	Key                 string
	BackupID            string
	FailedID            string
	HadPrevious         bool
}

type RollbackReplaceResult struct {
	Key              string
	TargetPath       string
	QuarantinePath   string
	RestoredPrevious bool
}

type Result struct {
	Key         string
	TargetPath  string
	BackupPath  string
	HadPrevious bool
}

type PruneResult struct {
	Removed []string
}

type Manager struct {
	root        string
	targetRoot  string
	stagingRoot string
	backupRoot  string
	mu          sync.Mutex
}

func New(options Options) (*Manager, error) {
	root := filepath.Clean(strings.TrimSpace(options.Root))
	if !filepath.IsAbs(root) {
		return nil, ErrInvalidOptions
	}
	targetDirectory := defaultDirectory(options.TargetDirectory, "installed")
	stagingDirectory := defaultDirectory(options.StagingDirectory, ".staging")
	backupDirectory := defaultDirectory(options.BackupDirectory, ".backups")
	if !validComponent(targetDirectory) || !validComponent(stagingDirectory) || !validComponent(backupDirectory) ||
		targetDirectory == stagingDirectory || targetDirectory == backupDirectory || stagingDirectory == backupDirectory {
		return nil, ErrInvalidOptions
	}
	manager := &Manager{
		root: root, targetRoot: filepath.Join(root, targetDirectory),
		stagingRoot: filepath.Join(root, stagingDirectory), backupRoot: filepath.Join(root, backupDirectory),
	}
	if err := manager.ensureRoots(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (manager *Manager) TargetRoot() string {
	if manager == nil {
		return ""
	}
	return manager.targetRoot
}

func (manager *Manager) TargetPath(key string) string {
	if manager == nil || !validComponent(key) {
		return ""
	}
	return filepath.Join(manager.targetRoot, key)
}

func (manager *Manager) StagingPath(id string) string {
	if manager == nil || !validComponent(id) {
		return ""
	}
	return filepath.Join(manager.stagingRoot, id)
}

func (manager *Manager) BackupPath(id string) string {
	if manager == nil || !validComponent(id) {
		return ""
	}
	return filepath.Join(manager.backupRoot, id)
}

func (manager *Manager) Replace(ctx context.Context, request ReplaceRequest) (Result, error) {
	if ctx == nil || !validComponent(request.Key) || !validComponent(request.StagingID) || !validComponent(request.BackupID) {
		return Result{}, ErrInvalidOptions
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := manager.ensureRoots(); err != nil {
		return Result{}, err
	}
	target := manager.TargetPath(request.Key)
	staging := manager.StagingPath(request.StagingID)
	backup := manager.BackupPath(request.BackupID)
	if err := validateDirectoryTree(ctx, staging); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{}, ErrStagingMissing
		}
		return Result{}, err
	}
	backupExists := false
	if err := validateDirectoryTree(ctx, backup); err == nil {
		backupExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("检查目录事务备份: %w", err)
	}
	result := Result{Key: request.Key, TargetPath: target}
	if err := validateDirectoryTree(ctx, target); err == nil {
		result.HadPrevious = true
		result.BackupPath = backup
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	if backupExists {
		if result.HadPrevious {
			return Result{}, ErrBackupConflict
		}
		result.HadPrevious = true
		result.BackupPath = backup
	}
	if result.HadPrevious {
		if !backupExists {
			if err := os.Rename(target, backup); err != nil {
				return Result{}, fmt.Errorf("备份当前目录: %w", err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		if result.HadPrevious {
			return Result{}, errors.Join(err, os.Rename(backup, target))
		}
		return Result{}, err
	}
	if err := os.Rename(staging, target); err != nil {
		if result.HadPrevious {
			restoreErr := os.Rename(backup, target)
			return Result{}, errors.Join(fmt.Errorf("提交暂存目录: %w", err), restoreErr)
		}
		return Result{}, fmt.Errorf("提交暂存目录: %w", err)
	}
	syncPaths := []string{manager.stagingRoot, manager.targetRoot}
	if result.HadPrevious {
		syncPaths = append(syncPaths, manager.backupRoot)
	}
	if err := syncDirectories(syncPaths...); err != nil {
		return result, fmt.Errorf("同步目录事务: %w", err)
	}
	return result, nil
}

func (manager *Manager) Remove(ctx context.Context, request RemoveRequest) (Result, error) {
	if ctx == nil || !validComponent(request.Key) || !validComponent(request.BackupID) {
		return Result{}, ErrInvalidOptions
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := manager.ensureRoots(); err != nil {
		return Result{}, err
	}
	target := manager.TargetPath(request.Key)
	backup := manager.BackupPath(request.BackupID)
	if err := validateDirectoryTree(ctx, target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{}, ErrTargetMissing
		}
		return Result{}, err
	}
	if request.Permanent {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		// 校验通过后删除确切目标，不生成可恢复副本；失败如实返回。
		if err := os.RemoveAll(target); err != nil {
			return Result{}, fmt.Errorf("永久删除目录: %w", err)
		}
		result := Result{Key: request.Key, TargetPath: target, HadPrevious: true}
		if err := syncDirectory(manager.targetRoot); err != nil {
			return result, fmt.Errorf("同步目录删除: %w", err)
		}
		return result, nil
	}
	if _, err := os.Lstat(backup); err == nil {
		return Result{}, ErrBackupConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("检查目录事务备份: %w", err)
	}
	if err := os.Rename(target, backup); err != nil {
		return Result{}, fmt.Errorf("卸载目录到备份: %w", err)
	}
	if err := syncDirectories(manager.targetRoot, manager.backupRoot); err != nil {
		return Result{Key: request.Key, TargetPath: target, BackupPath: backup, HadPrevious: true}, fmt.Errorf("同步目录事务: %w", err)
	}
	return Result{Key: request.Key, TargetPath: target, BackupPath: backup, HadPrevious: true}, nil
}

// RollbackReplace 撤销已经提交的目录替换，并隔离后删除失败版本。
func (manager *Manager) RollbackReplace(ctx context.Context, request RollbackReplaceRequest) (RollbackReplaceResult, error) {
	if ctx == nil || !validComponent(request.Key) || !validComponent(request.BackupID) || !validComponent(request.FailedID) ||
		request.BackupID == request.FailedID {
		return RollbackReplaceResult{}, ErrInvalidOptions
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return RollbackReplaceResult{}, err
	}
	if err := manager.ensureRoots(); err != nil {
		return RollbackReplaceResult{}, err
	}
	target := manager.TargetPath(request.Key)
	backup := manager.BackupPath(request.BackupID)
	quarantine := manager.StagingPath(request.FailedID)
	result := RollbackReplaceResult{Key: request.Key, TargetPath: target, QuarantinePath: quarantine}
	targetExists, err := safeDirectoryExists(ctx, target)
	if err != nil {
		return result, err
	}
	if request.RequireTargetAbsent && targetExists {
		return result, ErrRollbackConflict
	}
	backupExists, err := safeDirectoryExists(ctx, backup)
	if err != nil {
		return result, err
	}
	quarantineExists, err := safeDirectoryExists(ctx, quarantine)
	if err != nil {
		return result, err
	}
	if request.HadPrevious && targetExists && !backupExists && !quarantineExists {
		result.RestoredPrevious = true
		return result, nil
	}
	if quarantineExists && targetExists {
		if !request.HadPrevious || backupExists {
			return result, ErrRollbackConflict
		}
		result.RestoredPrevious = true
	}
	if request.HadPrevious && !backupExists && !targetExists {
		return result, ErrRollbackConflict
	}
	if !request.HadPrevious && backupExists {
		return result, ErrRollbackConflict
	}
	if !targetExists && !quarantineExists {
		if request.HadPrevious && backupExists {
			if err := os.Rename(backup, target); err != nil {
				return result, fmt.Errorf("恢复目录事务备份: %w", err)
			}
			result.RestoredPrevious = true
			return result, syncDirectories(manager.targetRoot, manager.backupRoot)
		}
		return result, nil
	}
	if targetExists && !result.RestoredPrevious {
		if err := os.Rename(target, quarantine); err != nil {
			return result, fmt.Errorf("隔离失败目录: %w", err)
		}
		quarantineExists = true
	}
	if request.HadPrevious && backupExists {
		if err := os.Rename(backup, target); err != nil {
			restoreErr := os.Rename(quarantine, target)
			return result, errors.Join(fmt.Errorf("恢复目录事务备份: %w", err), restoreErr)
		}
		result.RestoredPrevious = true
	}
	if err := syncDirectories(manager.targetRoot, manager.stagingRoot, manager.backupRoot); err != nil {
		return result, fmt.Errorf("同步目录事务回滚: %w", err)
	}
	if quarantineExists {
		if err := os.RemoveAll(quarantine); err != nil {
			return result, fmt.Errorf("清理失败目录: %w", err)
		}
		if err := syncDirectory(manager.stagingRoot); err != nil {
			return result, fmt.Errorf("同步失败目录清理: %w", err)
		}
	}
	return result, nil
}

// PruneBackups 删除严格早于截止时间的受控备份目录。
func (manager *Manager) PruneBackups(ctx context.Context, olderThan time.Time) (PruneResult, error) {
	if ctx == nil || olderThan.IsZero() {
		return PruneResult{}, ErrInvalidOptions
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return PruneResult{}, err
	}
	if err := manager.ensureRoots(); err != nil {
		return PruneResult{}, err
	}
	entries, err := os.ReadDir(manager.backupRoot)
	if err != nil {
		return PruneResult{}, fmt.Errorf("读取目录事务备份: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	result := PruneResult{Removed: []string{}}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !validComponent(entry.Name()) {
			return result, ErrUnsafePath
		}
		path := manager.BackupPath(entry.Name())
		if err := validateDirectoryTree(ctx, path); err != nil {
			return result, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return result, err
		}
		if !info.ModTime().Before(olderThan) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return result, fmt.Errorf("清理目录事务备份: %w", err)
		}
		result.Removed = append(result.Removed, entry.Name())
	}
	if len(result.Removed) > 0 {
		if err := syncDirectory(manager.backupRoot); err != nil {
			return result, fmt.Errorf("同步目录事务备份清理: %w", err)
		}
	}
	return result, nil
}

func (manager *Manager) ensureRoots() error {
	if manager == nil || !filepath.IsAbs(manager.root) {
		return ErrInvalidOptions
	}
	for _, path := range []string{manager.root, manager.targetRoot, manager.stagingRoot, manager.backupRoot} {
		if err := ensurePrivateDirectory(path); err != nil {
			return err
		}
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("创建目录事务目录: %w", err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafePath
	}
	return nil
}

func validateDirectoryTree(ctx context.Context, root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafePath
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return ErrUnsafePath
		}
		return nil
	})
}

func safeDirectoryExists(ctx context.Context, path string) (bool, error) {
	err := validateDirectoryTree(ctx, path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

func defaultDirectory(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func validComponent(value string) bool {
	return componentPattern.MatchString(strings.TrimSpace(value)) && strings.TrimSpace(value) == value
}

func syncDirectories(paths ...string) error {
	var result error
	for _, path := range paths {
		result = errors.Join(result, syncDirectory(path))
	}
	return result
}
