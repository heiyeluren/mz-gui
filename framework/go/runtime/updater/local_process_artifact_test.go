package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
)

func TestNewLocalProcessArtifactFetcherCopiesVerifiedFixedComponent(t *testing.T) {
	root := t.TempDir()
	payload := []byte("signed-desktop-payload")
	source := filepath.Join(root, ComponentDesktop+LocalProcessArtifactSuffix)
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	fetch, err := NewLocalProcessArtifactFetcher(root)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "desktop.bin")
	artifact := Artifact{Component: ComponentDesktop, Size: int64(len(payload)), SHA256: hexSHA256(payload)}
	if err := fetch(context.Background(), artifact, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("本地制品复制错误: got=%q err=%v", got, err)
	}
}

func TestNewLocalProcessArtifactFetcherRejectsSymlinkAndDigestMismatch(t *testing.T) {
	testkit.RequireSymlink(t)
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ComponentDesktop+LocalProcessArtifactSuffix)); err != nil {
		t.Fatal(err)
	}
	fetch, err := NewLocalProcessArtifactFetcher(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact := Artifact{Component: ComponentDesktop, Size: int64(len("outside")), SHA256: hexSHA256([]byte("outside"))}
	if err := fetch(context.Background(), artifact, filepath.Join(t.TempDir(), "dest.bin")); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("符号链接必须拒绝: %v", err)
	}

	if err := os.Remove(filepath.Join(root, ComponentDesktop+LocalProcessArtifactSuffix)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ComponentDesktop+LocalProcessArtifactSuffix), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fetch(context.Background(), artifact, filepath.Join(t.TempDir(), "dest.bin")); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("摘要或大小不匹配必须拒绝: %v", err)
	}
}

func TestNewLocalProcessArtifactFetcherRejectsUnsafeRootAndUnknownComponent(t *testing.T) {
	if _, err := NewLocalProcessArtifactFetcher("relative"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("相对更新根必须拒绝: %v", err)
	}
	root := t.TempDir()
	fetch, err := NewLocalProcessArtifactFetcher(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := fetch(context.Background(), Artifact{Component: "other", Size: 1, SHA256: hexSHA256([]byte("x"))}, filepath.Join(t.TempDir(), "dest.bin")); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("未知组件必须拒绝: %v", err)
	}
}
