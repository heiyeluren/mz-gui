// Package configfile 提供产品无关的私有配置文档存储机制。
package configfile

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const DefaultMaxBytes = 1 << 20

var (
	ErrInvalidOptions   = errors.New("配置文档存储参数无效")
	ErrRevisionConflict = errors.New("配置文档已被其他操作修改")
	ErrDocumentTooLarge = errors.New("配置文档超过大小上限")
	ErrSymbolicLink     = errors.New("配置文档不能是符号链接")
	ErrBackupConflict   = errors.New("配置文档备份与当前 revision 不一致")
)

type BackupPathFunc func(documentPath, revision string) string

type Options struct {
	Path          string
	MaxBytes      int
	Initial       []byte
	BackupPath    BackupPathFunc
	Registry      *Registry
	SchemaID      string
	SchemaVersion int
	LockedKeys    []string
}

type Snapshot struct {
	Data     []byte
	Revision string
}

type Store struct {
	options Options
	mu      sync.Mutex
}

func New(options Options) *Store {
	options.Initial = cloneBytes(options.Initial)
	return &Store{options: options}
}

// LoadExisting 安全读取已存在的配置文档，不创建缺失路径。
func LoadExisting(path string, maxBytes int) (Snapshot, error) {
	if path == "" || !filepath.IsAbs(path) || maxBytes <= 0 {
		return Snapshot{}, ErrInvalidOptions
	}
	data, err := readDocument(path, maxBytes)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot(data), nil
}

func (store *Store) LoadOrCreate() (Snapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.loadOrCreateLocked()
}

func (store *Store) Update(expectedRevision string, candidate []byte) (Snapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.validate(); err != nil {
		return Snapshot{}, err
	}
	candidate = cloneBytes(candidate)
	if len(candidate) > store.options.MaxBytes {
		return Snapshot{}, ErrDocumentTooLarge
	}
	current, err := store.loadOrCreateLocked()
	if err != nil {
		return Snapshot{}, err
	}
	if expectedRevision == "" || current.Revision != expectedRevision {
		return Snapshot{}, ErrRevisionConflict
	}
	if string(current.Data) == string(candidate) {
		return current, nil
	}
	if err := store.enforceGuards(current.Data, candidate); err != nil {
		return Snapshot{}, err
	}
	if err := store.backupLocked(current); err != nil {
		return Snapshot{}, err
	}
	if err := writePrivateAtomic(store.options.Path, candidate, store.options.MaxBytes); err != nil {
		return Snapshot{}, err
	}
	return snapshot(candidate), nil
}

func (store *Store) loadOrCreateLocked() (Snapshot, error) {
	if err := store.validate(); err != nil {
		return Snapshot{}, err
	}
	data, err := readDocument(store.options.Path, store.options.MaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		data = cloneBytes(store.options.Initial)
		if err := writePrivateAtomic(store.options.Path, data, store.options.MaxBytes); err != nil {
			return Snapshot{}, err
		}
	} else if err != nil {
		return Snapshot{}, err
	}
	return snapshot(data), nil
}

func (store *Store) enforceGuards(current, candidate []byte) error {
	if store.options.Registry != nil {
		if err := store.options.Registry.Validate(store.options.SchemaID, store.options.SchemaVersion, candidate); err != nil {
			return err
		}
	}
	if len(store.options.LockedKeys) > 0 {
		if err := EnforceTOMLLockedKeys(current, candidate, store.options.LockedKeys); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) validate() error {
	if store == nil || store.options.Path == "" || !filepath.IsAbs(store.options.Path) {
		return ErrInvalidOptions
	}
	if store.options.MaxBytes <= 0 {
		store.options.MaxBytes = DefaultMaxBytes
	}
	if len(store.options.Initial) > store.options.MaxBytes {
		return ErrDocumentTooLarge
	}
	return nil
}

func (store *Store) backupLocked(current Snapshot) error {
	if store.options.BackupPath == nil {
		return nil
	}
	path := store.options.BackupPath(store.options.Path, current.Revision)
	if path == "" || path == store.options.Path || !filepath.IsAbs(path) {
		return ErrInvalidOptions
	}
	if _, err := os.Lstat(path); err == nil {
		existing, readErr := readDocument(path, store.options.MaxBytes)
		if readErr != nil {
			return readErr
		}
		if Digest(existing) != current.Revision {
			return ErrBackupConflict
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("检查配置文档备份: %w", err)
	}
	if err := writePrivateAtomic(path, current.Data, store.options.MaxBytes); err != nil {
		return fmt.Errorf("保存配置文档备份: %w", err)
	}
	return nil
}

func readDocument(path string, maxBytes int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSymbolicLink
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("配置文档不是普通文件")
	}
	if info.Size() > int64(maxBytes) {
		return nil, ErrDocumentTooLarge
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("读取配置文档: %w", err)
	}
	if len(data) > maxBytes {
		return nil, ErrDocumentTooLarge
	}
	return data, nil
}

func writePrivateAtomic(path string, data []byte, maxBytes int) error {
	if len(data) > maxBytes {
		return ErrDocumentTooLarge
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("创建配置文档目录: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("设置配置文档目录权限: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".config-document-*.tmp")
	if err != nil {
		return fmt.Errorf("创建配置文档临时文件: %w", err)
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
		return fmt.Errorf("设置配置文档权限: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("写入配置文档临时文件: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("同步配置文档临时文件: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭配置文档临时文件: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("原子替换配置文档: %w", err)
	}
	committed = true
	return nil
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum)
}

func snapshot(data []byte) Snapshot {
	copy := cloneBytes(data)
	return Snapshot{Data: copy, Revision: Digest(copy)}
}

func cloneBytes(data []byte) []byte {
	return append([]byte(nil), data...)
}
