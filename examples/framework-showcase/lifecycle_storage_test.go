package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShowcasePrunesExpiredBlobCache(t *testing.T) {
	dir := t.TempDir()
	service := &ShowcaseService{shellDir: dir}
	cache := filepath.Join(dir, showcaseBlobCacheName)
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	expired := filepath.Join(cache, "expired.bin")
	keep := filepath.Join(cache, "keep.bin")
	if err := os.WriteFile(expired, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(expired, past, past); err != nil {
		t.Fatal(err)
	}
	if err := service.prepareFrameworkShell(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(expired); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("过期 Blob 必须清理")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("未过期 Blob 必须保留")
	}
}
