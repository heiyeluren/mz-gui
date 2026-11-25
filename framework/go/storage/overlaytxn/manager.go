// Package overlaytxn 提供产品无关的受管文件覆盖事务。
package overlaytxn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

var (
	ErrInvalidOptions = errors.New("受管文件事务参数无效")
	ErrInvalidRequest = errors.New("受管文件事务请求无效")
	ErrUnsafeSource   = errors.New("受管文件事务源文件不安全")
	ErrUnsafeTarget   = errors.New("受管文件事务目标不安全")
	ErrConflict       = errors.New("受管文件事务标识冲突")
)

const (
	defaultStagingDirectory       = ".mz-gui-overlay-staging"
	defaultBackupDirectory        = ".mz-gui-overlay-backups"
	maximumFiles                  = 20_000
	maximumFileBytes        int64 = 1 << 30
	maximumTotalBytes       int64 = 4 << 30
)

var componentPattern = regexp.MustCompile(`^\.?[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Options struct {
	Root             string
	StagingDirectory string
	BackupDirectory  string
}

type File struct {
	RelativePath string
	Executable   bool
}

type ApplyRequest struct {
	TransactionID string
	SourceRoot    string
	Files         []File
}

type Result struct {
	Applied    int
	BackedUp   int
	BackupPath string
}

type Manager struct {
	root        string
	stagingRoot string
	backupRoot  string
	mu          sync.Mutex
}

type appliedFile struct {
	relativePath string
	hadPrevious  bool
}

func New(options Options) (*Manager, error) {
	root := filepath.Clean(strings.TrimSpace(options.Root))
	stagingDirectory := defaultComponent(options.StagingDirectory, defaultStagingDirectory)
	backupDirectory := defaultComponent(options.BackupDirectory, defaultBackupDirectory)
	if !filepath.IsAbs(root) || !componentPattern.MatchString(stagingDirectory) || !componentPattern.MatchString(backupDirectory) || stagingDirectory == backupDirectory {
		return nil, ErrInvalidOptions
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOptions, err)
	}
	manager := &Manager{root: root, stagingRoot: filepath.Join(root, stagingDirectory), backupRoot: filepath.Join(root, backupDirectory)}
	if err := ensurePrivateDirectory(manager.stagingRoot); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOptions, err)
	}
	if err := ensurePrivateDirectory(manager.backupRoot); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOptions, err)
	}
	return manager, nil
}

func (manager *Manager) Apply(ctx context.Context, request ApplyRequest) (Result, error) {
	if ctx == nil || manager == nil || !componentPattern.MatchString(request.TransactionID) || len(request.Files) == 0 || len(request.Files) > maximumFiles {
		return Result{}, ErrInvalidRequest
	}
	sourceRoot := filepath.Clean(strings.TrimSpace(request.SourceRoot))
	if !filepath.IsAbs(sourceRoot) || !safeDirectory(sourceRoot) {
		return Result{}, ErrUnsafeSource
	}
	if err := validateFiles(request.Files); err != nil {
		return Result{}, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !safeDirectory(manager.root) || !safeDirectory(manager.stagingRoot) || !safeDirectory(manager.backupRoot) {
		return Result{}, ErrUnsafeTarget
	}
	stagingPath := filepath.Join(manager.stagingRoot, request.TransactionID)
	backupPath := filepath.Join(manager.backupRoot, request.TransactionID)
	if pathExists(stagingPath) || pathExists(backupPath) {
		return Result{}, ErrConflict
	}
	if err := os.Mkdir(stagingPath, 0o700); err != nil {
		return Result{}, fmt.Errorf("创建事务暂存目录: %w", err)
	}
	if err := stageFiles(ctx, sourceRoot, stagingPath, request.Files); err != nil {
		return Result{}, err
	}
	if err := os.Mkdir(backupPath, 0o700); err != nil {
		return Result{}, fmt.Errorf("创建事务备份目录: %w", err)
	}

	applied := make([]appliedFile, 0, len(request.Files))
	backedUp := 0
	for _, file := range request.Files {
		if err := ctx.Err(); err != nil {
			return Result{}, errors.Join(err, manager.rollback(applied, backupPath))
		}
		relative := filepath.FromSlash(file.RelativePath)
		target := filepath.Join(manager.root, relative)
		backup := filepath.Join(backupPath, relative)
		staged := filepath.Join(stagingPath, relative)
		if err := ensureSafeParent(manager.root, filepath.Dir(target)); err != nil {
			return Result{}, errors.Join(err, manager.rollback(applied, backupPath))
		}
		hadPrevious, err := movePrevious(target, backup, backupPath)
		if err != nil {
			return Result{}, errors.Join(err, manager.rollback(applied, backupPath))
		}
		if err := os.Rename(staged, target); err != nil {
			if hadPrevious {
				_ = os.Rename(backup, target)
			}
			return Result{}, errors.Join(fmt.Errorf("提交受管文件: %w", err), manager.rollback(applied, backupPath))
		}
		applied = append(applied, appliedFile{relativePath: relative, hadPrevious: hadPrevious})
		if hadPrevious {
			backedUp++
		}
	}
	_ = os.RemoveAll(stagingPath)
	return Result{Applied: len(applied), BackedUp: backedUp, BackupPath: backupPath}, nil
}

func (manager *Manager) rollback(applied []appliedFile, backupPath string) error {
	var rollbackError error
	for index := len(applied) - 1; index >= 0; index-- {
		item := applied[index]
		target := filepath.Join(manager.root, item.relativePath)
		failed := filepath.Join(backupPath, ".failed", item.relativePath)
		if err := ensurePrivateDirectory(filepath.Dir(failed)); err != nil {
			rollbackError = errors.Join(rollbackError, err)
			continue
		}
		if err := os.Rename(target, failed); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackError = errors.Join(rollbackError, err)
			continue
		}
		if item.hadPrevious {
			backup := filepath.Join(backupPath, item.relativePath)
			if err := ensureSafeParent(manager.root, filepath.Dir(target)); err != nil {
				rollbackError = errors.Join(rollbackError, err)
				continue
			}
			if err := os.Rename(backup, target); err != nil {
				rollbackError = errors.Join(rollbackError, err)
			}
		}
	}
	return rollbackError
}

func stageFiles(ctx context.Context, sourceRoot, stagingPath string, files []File) error {
	var total int64
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		relative := filepath.FromSlash(file.RelativePath)
		source := filepath.Join(sourceRoot, relative)
		if !safeParentChain(sourceRoot, filepath.Dir(source)) {
			return ErrUnsafeSource
		}
		info, err := os.Lstat(source)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 0 || info.Size() > maximumFileBytes {
			return ErrUnsafeSource
		}
		total += info.Size()
		if total > maximumTotalBytes {
			return ErrUnsafeSource
		}
		target := filepath.Join(stagingPath, relative)
		if err := ensurePrivateDirectory(filepath.Dir(target)); err != nil {
			return fmt.Errorf("创建暂存父目录: %w", err)
		}
		mode := os.FileMode(0o600)
		if file.Executable {
			mode = 0o700
		}
		if err := copyRegularFile(source, target, mode); err != nil {
			return err
		}
	}
	return nil
}

func movePrevious(target, backup, backupRoot string) (bool, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrUnsafeTarget
	}
	if err := ensurePrivateDirectory(filepath.Dir(backup)); err != nil || !pathWithin(backupRoot, backup) {
		return false, ErrUnsafeTarget
	}
	if err := os.Rename(target, backup); err != nil {
		return false, fmt.Errorf("备份旧受管文件: %w", err)
	}
	return true, nil
}

func validateFiles(files []File) error {
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if !safeRelativePath(file.RelativePath) {
			return ErrInvalidRequest
		}
		if _, exists := seen[file.RelativePath]; exists {
			return ErrInvalidRequest
		}
		seen[file.RelativePath] = struct{}{}
	}
	return nil
}

func safeRelativePath(value string) bool {
	if value == "" || len(value) > 4096 || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return false
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func ensureSafeParent(root, directory string) error {
	root, directory = filepath.Clean(root), filepath.Clean(directory)
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || !safeDirectory(root) {
		return ErrUnsafeTarget
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return fmt.Errorf("创建目标父目录: %w", err)
			}
			continue
		}
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafeTarget
		}
	}
	return nil
}

func safeParentChain(root, directory string) bool {
	root, directory = filepath.Clean(root), filepath.Clean(directory)
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || !safeDirectory(root) {
		return false
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

func copyRegularFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("打开暂存源文件: %w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("创建暂存文件: %w", err)
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(copyErr, syncErr, closeErr)
	}
	return nil
}

func ensurePrivateDirectory(target string) error {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(target, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(target)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafeTarget
	}
	return os.Chmod(target, 0o700)
}

func safeDirectory(target string) bool {
	info, err := os.Lstat(target)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func pathExists(target string) bool {
	_, err := os.Lstat(target)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func defaultComponent(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
