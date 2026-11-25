// Package workspacefile 提供产品无关的授权根目录文件读写 Broker。
package workspacefile

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	defaultMaxReadBytes  int64 = 2 << 20
	defaultMaxWriteBytes int64 = 2 << 20
	maxRelativePathBytes       = 4096
)

var (
	ErrInvalidOptions = errors.New("工作区文件 Broker 参数无效")
	ErrUnsafePath     = errors.New("工作区文件路径不安全")
	ErrSymbolicLink   = errors.New("工作区文件路径包含符号链接")
	ErrNotFound       = errors.New("工作区文件不存在")
	ErrNotRegular     = errors.New("工作区目标不是普通文件")
	ErrTooLarge       = errors.New("工作区文件超过大小上限")
	ErrConflict       = errors.New("工作区文件内容发生冲突")

	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Options struct {
	MaxReadBytes  int64
	MaxWriteBytes int64
}

type ReadResult struct {
	Content   []byte
	SHA256    string
	SizeBytes int64
}

type WriteResult struct {
	SHA256    string
	SizeBytes int64
	Created   bool
}

type Broker struct {
	maxReadBytes  int64
	maxWriteBytes int64
	mu            sync.RWMutex
}

func New(options Options) (*Broker, error) {
	if options.MaxReadBytes < 0 || options.MaxWriteBytes < 0 {
		return nil, ErrInvalidOptions
	}
	if options.MaxReadBytes == 0 {
		options.MaxReadBytes = defaultMaxReadBytes
	}
	if options.MaxWriteBytes == 0 {
		options.MaxWriteBytes = defaultMaxWriteBytes
	}
	return &Broker{maxReadBytes: options.MaxReadBytes, maxWriteBytes: options.MaxWriteBytes}, nil
}

func (broker *Broker) ReadFile(ctx context.Context, root, relativePath string) (ReadResult, error) {
	if broker == nil || ctx == nil {
		return ReadResult{}, ErrInvalidOptions
	}
	broker.mu.RLock()
	defer broker.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return ReadResult{}, err
	}
	rootHandle, target, _, err := resolvePath(root, relativePath, false)
	if err != nil {
		return ReadResult{}, err
	}
	defer rootHandle.Close()
	return readRegularFile(ctx, rootHandle, target, broker.maxReadBytes)
}

func (broker *Broker) WriteFile(ctx context.Context, root, relativePath string, content []byte, expectedSHA256 string) (WriteResult, error) {
	if broker == nil || ctx == nil {
		return WriteResult{}, ErrInvalidOptions
	}
	if int64(len(content)) > broker.maxWriteBytes {
		return WriteResult{}, ErrTooLarge
	}
	expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	if expectedSHA256 != "" && !digestPattern.MatchString(expectedSHA256) {
		return WriteResult{}, ErrConflict
	}

	broker.mu.Lock()
	defer broker.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return WriteResult{}, err
	}
	rootHandle, target, exists, err := resolvePath(root, relativePath, true)
	if err != nil {
		return WriteResult{}, err
	}
	defer rootHandle.Close()
	mode := os.FileMode(0o600)
	if exists {
		current, err := readRegularFile(ctx, rootHandle, target, broker.maxWriteBytes)
		if err != nil {
			return WriteResult{}, err
		}
		if expectedSHA256 == "" || current.SHA256 != expectedSHA256 {
			return WriteResult{}, ErrConflict
		}
		info, err := rootHandle.Lstat(target)
		if err != nil {
			return WriteResult{}, classifyPathError(err)
		}
		mode = info.Mode().Perm()
	} else if expectedSHA256 != "" {
		return WriteResult{}, ErrConflict
	}

	parent := filepath.Dir(target)
	temporary, temporaryPath, err := createTemporary(rootHandle, parent, mode)
	if err != nil {
		return WriteResult{}, fmt.Errorf("创建工作区文件暂存项: %w", err)
	}
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = rootHandle.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(content); err != nil {
		return WriteResult{}, fmt.Errorf("写入工作区文件暂存项: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return WriteResult{}, fmt.Errorf("同步工作区文件暂存项: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return WriteResult{}, fmt.Errorf("关闭工作区文件暂存项: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return WriteResult{}, err
	}

	// 提交前再次校验，避免 Broker 操作期间的常见并发覆盖。
	stillExists, err := validateRootPath(rootHandle, target, true)
	if err != nil {
		return WriteResult{}, err
	}
	if stillExists != exists {
		return WriteResult{}, ErrConflict
	}
	if exists {
		current, err := readRegularFile(ctx, rootHandle, target, broker.maxWriteBytes)
		if err != nil {
			return WriteResult{}, err
		}
		if current.SHA256 != expectedSHA256 {
			return WriteResult{}, ErrConflict
		}
	}
	if err := commitFile(rootHandle, temporaryPath, target, exists); err != nil {
		if errors.Is(err, os.ErrExist) {
			return WriteResult{}, ErrConflict
		}
		return WriteResult{}, fmt.Errorf("提交工作区文件: %w", err)
	}
	committed = true
	if err := syncDirectory(rootHandle, parent); err != nil {
		return WriteResult{}, fmt.Errorf("同步工作区文件目录: %w", err)
	}
	value := sha256.Sum256(content)
	return WriteResult{SHA256: hex.EncodeToString(value[:]), SizeBytes: int64(len(content)), Created: !exists}, nil
}

func createTemporary(root *os.Root, parent string, mode os.FileMode) (*os.File, string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return nil, "", err
		}
		name := filepath.Join(parent, ".mzgui-write-"+hex.EncodeToString(random))
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		if err := file.Chmod(mode); err != nil {
			_ = file.Close()
			_ = root.Remove(name)
			return nil, "", err
		}
		return file, name, nil
	}
	return nil, "", os.ErrExist
}

func readRegularFile(ctx context.Context, root *os.Root, target string, maxBytes int64) (ReadResult, error) {
	file, err := root.Open(target)
	if err != nil {
		return ReadResult{}, classifyPathError(err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return ReadResult{}, classifyPathError(err)
	}
	if !opened.Mode().IsRegular() {
		return ReadResult{}, ErrNotRegular
	}
	if opened.Size() > maxBytes {
		return ReadResult{}, ErrTooLarge
	}
	pathInfo, err := root.Lstat(target)
	if err != nil {
		return ReadResult{}, classifyPathError(err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return ReadResult{}, ErrSymbolicLink
	}
	if !os.SameFile(opened, pathInfo) {
		return ReadResult{}, ErrConflict
	}
	reader := io.LimitReader(&contextReader{ctx: ctx, reader: file}, maxBytes+1)
	content, err := io.ReadAll(reader)
	if err != nil {
		return ReadResult{}, err
	}
	if int64(len(content)) > maxBytes {
		return ReadResult{}, ErrTooLarge
	}
	after, err := file.Stat()
	if err != nil {
		return ReadResult{}, classifyPathError(err)
	}
	if after.Size() != opened.Size() || after.ModTime() != opened.ModTime() {
		return ReadResult{}, ErrConflict
	}
	value := sha256.Sum256(content)
	return ReadResult{Content: content, SHA256: hex.EncodeToString(value[:]), SizeBytes: int64(len(content))}, nil
}

func resolvePath(root, relativePath string, allowMissingTarget bool) (*os.Root, string, bool, error) {
	cleanRoot := filepath.Clean(strings.TrimSpace(root))
	if cleanRoot == "" || !filepath.IsAbs(cleanRoot) {
		return nil, "", false, ErrUnsafePath
	}
	rootInfo, err := os.Lstat(cleanRoot)
	if err != nil {
		return nil, "", false, classifyPathError(err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, "", false, ErrSymbolicLink
	}
	if !rootInfo.IsDir() {
		return nil, "", false, ErrUnsafePath
	}
	canonicalRoot, err := filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		return nil, "", false, classifyPathError(err)
	}
	parts, err := validateRelativePath(relativePath)
	if err != nil {
		return nil, "", false, err
	}
	rootHandle, err := os.OpenRoot(canonicalRoot)
	if err != nil {
		return nil, "", false, classifyPathError(err)
	}
	openedInfo, err := rootHandle.Stat(".")
	if err != nil || !os.SameFile(rootInfo, openedInfo) {
		rootHandle.Close()
		return nil, "", false, ErrConflict
	}
	target := filepath.Join(parts...)
	exists, err := validateRootPath(rootHandle, target, allowMissingTarget)
	if err != nil {
		rootHandle.Close()
		return nil, "", false, err
	}
	return rootHandle, target, exists, nil
}

func validateRootPath(root *os.Root, target string, allowMissingTarget bool) (bool, error) {
	parts := strings.Split(filepath.ToSlash(target), "/")
	current := ""
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := root.Lstat(current)
		last := index == len(parts)-1
		if errors.Is(statErr, os.ErrNotExist) && last && allowMissingTarget {
			return false, nil
		}
		if statErr != nil {
			return false, classifyPathError(statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, ErrSymbolicLink
		}
		if !last && !info.IsDir() {
			return false, ErrUnsafePath
		}
		if last && !info.Mode().IsRegular() {
			return false, ErrNotRegular
		}
	}
	return true, nil
}

func validateRelativePath(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxRelativePathBytes || filepath.IsAbs(value) || filepath.VolumeName(value) != "" || strings.ContainsAny(value, "\x00\r\n") {
		return nil, ErrUnsafePath
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	if strings.HasPrefix(normalized, "/") || strings.HasSuffix(normalized, "/") {
		return nil, ErrUnsafePath
	}
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		if !validPortableComponent(part) {
			return nil, ErrUnsafePath
		}
	}
	return parts, nil
}

func validPortableComponent(value string) bool {
	if value == "" || value == "." || value == ".." || strings.Contains(value, ":") || strings.TrimRight(value, " .") != value {
		return false
	}
	for _, char := range value {
		if char < 0x20 {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(value, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	default:
		return true
	}
}

func classifyPathError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
