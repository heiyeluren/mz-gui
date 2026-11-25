package updater

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidDownloadedArtifact = errors.New("下载制品校验失败")

// DownloadedArtifactPolicy 描述由上游签名清单和持久任务共同绑定的下载制品。
type DownloadedArtifactPolicy struct {
	Root              string
	Path              string
	SHA256            string
	Size              int64
	AllowedExtensions []string
}

// VerifiedDownloadedArtifact 是可交给平台安装适配器的只读校验结果。
type VerifiedDownloadedArtifact struct {
	Path   string
	SHA256 string
	Size   int64
}

// VerifyDownloadedArtifact 在安装动作前重新验证路径、类型、大小和摘要。
func VerifyDownloadedArtifact(policy DownloadedArtifactPolicy) (VerifiedDownloadedArtifact, error) {
	root := filepath.Clean(strings.TrimSpace(policy.Root))
	path := filepath.Clean(strings.TrimSpace(policy.Path))
	expected := strings.ToLower(strings.TrimSpace(policy.SHA256))
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) || !sha256Pattern.MatchString(expected) || policy.Size < 1 || len(policy.AllowedExtensions) == 0 {
		return VerifiedDownloadedArtifact{}, ErrInvalidDownloadedArtifact
	}
	if err := requireSafeDirectory(root); err != nil {
		return VerifiedDownloadedArtifact{}, fmt.Errorf("%w: 更新根目录不安全", ErrInvalidDownloadedArtifact)
	}
	if !pathWithinRoot(root, path) || path == root {
		return VerifiedDownloadedArtifact{}, fmt.Errorf("%w: 制品路径越界", ErrInvalidDownloadedArtifact)
	}
	if err := requireSafeAncestors(root, filepath.Dir(path)); err != nil {
		return VerifiedDownloadedArtifact{}, fmt.Errorf("%w: 制品父目录不安全", ErrInvalidDownloadedArtifact)
	}
	extension := strings.ToLower(filepath.Ext(path))
	allowed := false
	for _, candidate := range policy.AllowedExtensions {
		if extension == strings.ToLower(strings.TrimSpace(candidate)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return VerifiedDownloadedArtifact{}, fmt.Errorf("%w: 制品扩展名不在白名单", ErrInvalidDownloadedArtifact)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != policy.Size {
		return VerifiedDownloadedArtifact{}, fmt.Errorf("%w: 制品类型或大小不一致", ErrInvalidDownloadedArtifact)
	}
	digest, err := hashFile(path)
	if err != nil || subtle.ConstantTimeCompare([]byte(digest), []byte(expected)) != 1 {
		return VerifiedDownloadedArtifact{}, fmt.Errorf("%w: 制品摘要不一致", ErrInvalidDownloadedArtifact)
	}
	return VerifiedDownloadedArtifact{Path: path, SHA256: digest, Size: info.Size()}, nil
}

func requireSafeDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("目录类型不安全")
	}
	return nil
}

func requireSafeAncestors(root, directory string) error {
	if !pathWithinRoot(root, directory) {
		return errors.New("目录越界")
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil {
		return err
	}
	cursor := root
	if relative == "." {
		return nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		cursor = filepath.Join(cursor, part)
		if err := requireSafeDirectory(cursor); err != nil {
			return err
		}
	}
	return nil
}

func pathWithinRoot(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
