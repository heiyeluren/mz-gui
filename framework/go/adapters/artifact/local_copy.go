package artifact

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrLocalArtifactInvalid  = errors.New("本机制品参数或文件无效")
	ErrLocalArtifactDigest   = errors.New("本机制品 SHA-256 不匹配")
	ErrLocalArtifactSize     = errors.New("本机制品大小不匹配")
	ErrLocalArtifactConflict = errors.New("本机制品目标发生冲突")
)

type CopyResult struct {
	Bytes   int64
	SHA256  string
	Applied bool
}

// CopyAndVerify 将普通文件原子复制到受控目录，并在发布前复验大小与 SHA-256。
func CopyAndVerify(ctx context.Context, source, destination, expectedSHA256 string, expectedBytes, maximumBytes int64) (CopyResult, error) {
	source = filepath.Clean(strings.TrimSpace(source))
	destination = filepath.Clean(strings.TrimSpace(destination))
	expected, err := normalizeSHA256(expectedSHA256)
	if ctx == nil || err != nil || !filepath.IsAbs(source) || !filepath.IsAbs(destination) || source == destination ||
		expectedBytes < 1 || maximumBytes < expectedBytes {
		return CopyResult{}, ErrLocalArtifactInvalid
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil || !sourceInfo.Mode().IsRegular() || sourceInfo.Mode()&os.ModeSymlink != 0 {
		return CopyResult{}, errors.Join(ErrLocalArtifactInvalid, err)
	}
	if sourceInfo.Size() != expectedBytes || sourceInfo.Size() > maximumBytes {
		return CopyResult{}, ErrLocalArtifactSize
	}
	if existing, exists, existingErr := verifyExistingLocalArtifact(destination, expected, expectedBytes); existingErr != nil {
		return CopyResult{}, existingErr
	} else if exists {
		return existing, nil
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return CopyResult{}, fmt.Errorf("创建本机制品目录: %w", err)
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return CopyResult{}, errors.Join(ErrLocalArtifactInvalid, err)
	}
	sourceFile, err := os.Open(source)
	if err != nil {
		return CopyResult{}, err
	}
	defer sourceFile.Close()
	openedInfo, err := sourceFile.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(sourceInfo, openedInfo) || openedInfo.Size() != expectedBytes {
		return CopyResult{}, errors.Join(ErrLocalArtifactInvalid, err)
	}
	temporary, err := os.CreateTemp(parent, ".artifact-copy-*")
	if err != nil {
		return CopyResult{}, fmt.Errorf("创建本机制品临时文件: %w", err)
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
		return CopyResult{}, err
	}
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hasher), &contextReader{ctx: ctx, reader: io.LimitReader(sourceFile, maximumBytes+1)})
	if err != nil {
		return CopyResult{}, err
	}
	if written != expectedBytes {
		return CopyResult{}, ErrLocalArtifactSize
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return CopyResult{}, ErrLocalArtifactDigest
	}
	if err := temporary.Sync(); err != nil {
		return CopyResult{}, err
	}
	if err := temporary.Close(); err != nil {
		return CopyResult{}, err
	}
	if _, err := os.Lstat(destination); err == nil {
		return CopyResult{}, ErrLocalArtifactConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return CopyResult{}, err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return CopyResult{}, fmt.Errorf("发布本机制品: %w", err)
	}
	committed = true
	return CopyResult{Bytes: written, SHA256: actual, Applied: true}, nil
}

func verifyExistingLocalArtifact(path, expectedSHA256 string, expectedBytes int64) (CopyResult, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return CopyResult{}, false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != expectedBytes {
		return CopyResult{}, false, errors.Join(ErrLocalArtifactConflict, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return CopyResult{}, false, errors.Join(ErrLocalArtifactConflict, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return CopyResult{}, false, errors.Join(ErrLocalArtifactConflict, err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return CopyResult{}, false, errors.Join(ErrLocalArtifactConflict, err)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expectedSHA256)) != 1 {
		return CopyResult{}, false, ErrLocalArtifactConflict
	}
	return CopyResult{Bytes: expectedBytes, SHA256: actual}, true, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, reader.ctx.Err()
	default:
		return reader.reader.Read(buffer)
	}
}
