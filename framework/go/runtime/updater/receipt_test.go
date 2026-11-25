package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyReceiptAcceptsMatchingFilesAndRejectsTamper(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bin", "mz-gui-helper")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("helper-bytes")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	receipt := Receipt{
		Product: "mz-gui", Version: "1.0.0",
		Files: []ReceiptFile{{RelPath: filepath.Join("bin", "mz-gui-helper"), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(payload))}},
	}
	if err := VerifyReceipt(root, receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyReceipt(root, receipt); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatalf("篡改回执错误 = %v", err)
	}
	if err := VerifyReceipt(root, Receipt{Product: "mz-gui", Version: "1.0.0", Files: []ReceiptFile{{RelPath: "../escape", SHA256: hex.EncodeToString(sum[:]), Size: 1}}}); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatalf("越权回执必须拒绝: %v", err)
	}
}

func TestBuildWriteAndLoadReceiptRoundTrip(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("bin", "mz-gui")
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("install-bytes")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := BuildReceipt("mz-gui", "1.2.0", root, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	receipt.SBOMSHA256 = hexSHA256([]byte("sbom-lite"))
	dest := filepath.Join(root, "receipt.json")
	if err := WriteReceipt(dest, receipt); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReceipt(dest)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Product != "mz-gui" || loaded.Version != "1.2.0" || loaded.SBOMSHA256 != receipt.SBOMSHA256 || len(loaded.Files) != 1 {
		t.Fatalf("回执往返错误: %#v", loaded)
	}
	if err := VerifyReceipt(root, loaded); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildReceipt("mz-gui", "1.2.0", root, []string{"../escape"}); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatalf("越权构建必须拒绝: %v", err)
	}
	if err := WriteReceipt(filepath.Join(root, "bad.json"), Receipt{Product: "mz-gui", Version: "1.2.0"}); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatalf("空文件回执必须拒绝: %v", err)
	}
}
