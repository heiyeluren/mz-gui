package updater

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidReceipt = errors.New("安装回执无效")

type ReceiptFile struct {
	RelPath string `json:"rel_path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

type Receipt struct {
	Product    string        `json:"product"`
	Version    string        `json:"version"`
	SBOMSHA256 string        `json:"sbom_sha256,omitempty"`
	Files      []ReceiptFile `json:"files"`
}

func BuildReceipt(product, version, root string, relPaths []string) (Receipt, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || !filepath.IsAbs(root) || strings.TrimSpace(product) == "" ||
		strings.TrimSpace(version) == "" || len(relPaths) == 0 {
		return Receipt{}, ErrInvalidReceipt
	}
	files := make([]ReceiptFile, 0, len(relPaths))
	seen := make(map[string]struct{}, len(relPaths))
	for _, raw := range relPaths {
		rel := filepath.Clean(strings.TrimSpace(raw))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return Receipt{}, ErrInvalidReceipt
		}
		if _, exists := seen[rel]; exists {
			return Receipt{}, ErrInvalidReceipt
		}
		seen[rel] = struct{}{}
		path := filepath.Join(root, rel)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 {
			return Receipt{}, fmt.Errorf("%w: %s", ErrInvalidReceipt, rel)
		}
		digest, err := hashReceiptFile(path, info.Size())
		if err != nil {
			return Receipt{}, fmt.Errorf("%w: %s", ErrInvalidReceipt, rel)
		}
		files = append(files, ReceiptFile{RelPath: rel, SHA256: digest, Size: info.Size()})
	}
	return Receipt{Product: product, Version: version, Files: files}, nil
}

func WriteReceipt(path string, receipt Receipt) error {
	if err := validateReceiptShape(path, receipt); err != nil {
		return err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidReceipt, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidReceipt, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidReceipt, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: %v", ErrInvalidReceipt, err)
	}
	return nil
}

func LoadReceipt(path string) (Receipt, error) {
	if path == "" || !filepath.IsAbs(path) {
		return Receipt{}, ErrInvalidReceipt
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrInvalidReceipt, err)
	}
	var receipt Receipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrInvalidReceipt, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Receipt{}, fmt.Errorf("%w: 回执包含尾随内容", ErrInvalidReceipt)
	}
	if err := validateReceiptShape(path, receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func VerifyReceipt(root string, receipt Receipt) error {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || !filepath.IsAbs(root) || strings.TrimSpace(receipt.Product) == "" ||
		strings.TrimSpace(receipt.Version) == "" || len(receipt.Files) == 0 {
		return ErrInvalidReceipt
	}
	if receipt.SBOMSHA256 != "" && !sha256Pattern.MatchString(receipt.SBOMSHA256) {
		return ErrInvalidReceipt
	}
	seen := make(map[string]struct{}, len(receipt.Files))
	for _, file := range receipt.Files {
		rel := filepath.Clean(strings.TrimSpace(file.RelPath))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) ||
			!sha256Pattern.MatchString(file.SHA256) || file.Size < 1 {
			return ErrInvalidReceipt
		}
		if _, exists := seen[rel]; exists {
			return ErrInvalidReceipt
		}
		seen[rel] = struct{}{}
		path := filepath.Join(root, rel)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != file.Size {
			return fmt.Errorf("%w: %s", ErrInvalidReceipt, rel)
		}
		digest, err := hashReceiptFile(path, file.Size)
		if err != nil || digest != file.SHA256 {
			return fmt.Errorf("%w: %s", ErrInvalidReceipt, rel)
		}
	}
	return nil
}

func validateReceiptShape(path string, receipt Receipt) error {
	if path == "" || !filepath.IsAbs(path) || strings.TrimSpace(receipt.Product) == "" ||
		strings.TrimSpace(receipt.Version) == "" || len(receipt.Files) == 0 {
		return ErrInvalidReceipt
	}
	if receipt.SBOMSHA256 != "" && !sha256Pattern.MatchString(receipt.SBOMSHA256) {
		return ErrInvalidReceipt
	}
	seen := make(map[string]struct{}, len(receipt.Files))
	for _, file := range receipt.Files {
		rel := filepath.Clean(strings.TrimSpace(file.RelPath))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) ||
			!sha256Pattern.MatchString(file.SHA256) || file.Size < 1 {
			return ErrInvalidReceipt
		}
		if _, exists := seen[rel]; exists {
			return ErrInvalidReceipt
		}
		seen[rel] = struct{}{}
	}
	return nil
}

func hashReceiptFile(path string, size int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	written, err := io.Copy(sum, io.LimitReader(file, size+1))
	if err != nil || written != size {
		return "", ErrInvalidReceipt
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
