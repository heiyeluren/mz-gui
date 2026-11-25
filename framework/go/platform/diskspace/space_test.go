package diskspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAvailableBytesForMissingDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "not-created", "engine")
	if _, err := AvailableBytes(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("查询不应创建目录")
	}
}

func TestAvailableBytesRejectsInvalidPaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "relative", file, filepath.Join(file, "child")} {
		if _, err := AvailableBytes(path); err == nil {
			t.Fatalf("应拒绝非法目录 %q", path)
		}
	}
}
