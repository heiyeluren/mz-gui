package updater

import (
	"context"
	"fmt"
	"path/filepath"
)

// LocalProcessArtifactSuffix 是 helper 受控更新目录内唯一允许的进程载荷后缀。
const LocalProcessArtifactSuffix = ".payload"

// NewLocalProcessArtifactFetcher 创建只读取固定组件文件名的本机制品读取器。
// 调用方不能传入来源路径，来源始终为 <root>/<component>.payload。
func NewLocalProcessArtifactFetcher(root string) (func(context.Context, Artifact, string) error, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) || requireSafeDirectory(root) != nil {
		return nil, ErrInvalidSession
	}
	return func(ctx context.Context, artifact Artifact, destination string) error {
		if ctx == nil || ctx.Err() != nil || !validProcessComponent(artifact.Component) || artifact.Component == "" ||
			artifact.Size < 1 || !sha256Pattern.MatchString(artifact.SHA256) || !filepath.IsAbs(destination) {
			return ErrInvalidSession
		}
		if err := requireSafeDirectory(root); err != nil {
			return ErrInvalidSession
		}
		source := filepath.Join(root, artifact.Component+LocalProcessArtifactSuffix)
		if !pathWithinRoot(root, source) || source == root {
			return ErrInvalidSession
		}
		if _, err := absRegularFile(source); err != nil {
			return fmt.Errorf("%w: 本地组件制品类型不安全", ErrInvalidSession)
		}
		if err := verifyArtifactFile(source, artifact); err != nil {
			return err
		}
		if err := copyExclusive(source, destination, maximumArtifactBytes); err != nil {
			return fmt.Errorf("%w: 复制本地组件制品失败: %v", ErrInvalidSession, err)
		}
		return nil
	}, nil
}
