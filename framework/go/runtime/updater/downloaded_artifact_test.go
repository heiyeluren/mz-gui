package updater

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyDownloadedArtifactAcceptsOnlyWhitelistedVerifiedFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "updates")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("signed-installer-fixture")
	path := filepath.Join(root, "mozi-1.5.0.pkg")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	verified, err := VerifyDownloadedArtifact(DownloadedArtifactPolicy{
		Root: root, Path: path, SHA256: hexSHA256(payload), Size: int64(len(payload)),
		AllowedExtensions: []string{".pkg", ".dmg"},
	})
	if err != nil || verified.Path != path || verified.SHA256 != hexSHA256(payload) {
		t.Fatalf("签名任务产物应通过二次校验: verified=%#v err=%v", verified, err)
	}
}

func TestVerifyDownloadedArtifactRejectsEscapeSymlinkDigestAndSize(t *testing.T) {
	root := filepath.Join(t.TempDir(), "updates")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("installer")
	outside := filepath.Join(t.TempDir(), "outside.pkg")
	if err := os.WriteFile(outside, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	base := DownloadedArtifactPolicy{
		Root: root, Path: outside, SHA256: hexSHA256(payload), Size: int64(len(payload)),
		AllowedExtensions: []string{".pkg"},
	}
	if _, err := VerifyDownloadedArtifact(base); !errors.Is(err, ErrInvalidDownloadedArtifact) {
		t.Fatalf("更新根外路径必须拒绝: %v", err)
	}

	linked := filepath.Join(root, "linked.pkg")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	base.Path = linked
	if _, err := VerifyDownloadedArtifact(base); !errors.Is(err, ErrInvalidDownloadedArtifact) {
		t.Fatalf("符号链接安装包必须拒绝: %v", err)
	}

	inside := filepath.Join(root, "inside.pkg")
	if err := os.WriteFile(inside, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	base.Path = inside
	base.SHA256 = hexSHA256([]byte("tampered"))
	if _, err := VerifyDownloadedArtifact(base); !errors.Is(err, ErrInvalidDownloadedArtifact) {
		t.Fatalf("摘要不匹配必须拒绝: %v", err)
	}
	base.SHA256 = hexSHA256(payload)
	base.Size++
	if _, err := VerifyDownloadedArtifact(base); !errors.Is(err, ErrInvalidDownloadedArtifact) {
		t.Fatalf("大小不匹配必须拒绝: %v", err)
	}
}
