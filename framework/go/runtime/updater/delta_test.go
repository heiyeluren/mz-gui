package updater

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeAndApplyDeltaReconstructsTargetAndRejectsWrongBase(t *testing.T) {
	base := []byte("BASE-CONTENT-AAAA")
	target := []byte("BASE-CONTENT-AAAA-NEW-TAIL")
	encoded, err := EncodeDelta(base, target)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(encoded, target) || !bytes.Contains(encoded, []byte(deltaMagic)) {
		t.Fatalf("差分包应使用受控编码而不是原文: size=%d", len(encoded))
	}

	root := t.TempDir()
	basePath := filepath.Join(root, "live.bin")
	deltaPath := filepath.Join(root, "patch.bin")
	destPath := filepath.Join(root, "out", "staged.bin")
	if err := os.WriteFile(basePath, base, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deltaPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyDelta(basePath, deltaPath, destPath); err != nil {
		t.Fatalf("应用差分: %v", err)
	}
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, target) {
		t.Fatalf("差分结果错误: got=%q want=%q", got, target)
	}
	if hexSHA256(got) != hexSHA256(target) {
		t.Fatal("差分结果摘要不一致")
	}

	wrongBase := filepath.Join(root, "wrong.bin")
	if err := os.WriteFile(wrongBase, []byte("OTHER-BASE"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyDelta(wrongBase, deltaPath, filepath.Join(root, "out", "bad.bin")); !errors.Is(err, ErrDeltaBaseMismatch) {
		t.Fatalf("错误基础摘要应失败: %v", err)
	}
}

func TestEncodeDeltaRejectsEmptyTargetAndApplyRejectsTruncatedPayload(t *testing.T) {
	if _, err := EncodeDelta([]byte("base"), nil); !errors.Is(err, ErrInvalidDelta) {
		t.Fatalf("空目标应拒绝: %v", err)
	}
	root := t.TempDir()
	basePath := filepath.Join(root, "live.bin")
	deltaPath := filepath.Join(root, "bad.bin")
	if err := os.WriteFile(basePath, []byte("BASE"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deltaPath, []byte("MZGUID1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyDelta(basePath, deltaPath, filepath.Join(root, "staged.bin")); !errors.Is(err, ErrInvalidDelta) {
		t.Fatalf("截断差分应拒绝: %v", err)
	}
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
