package clipboard

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestNormalizeFilePathsAcceptsAbsolutePathsAndDeduplicates(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "shot.png")
	second := filepath.Join(root, "notes.txt")

	paths, err := NormalizeFilePaths([]string{first, filepath.Join(root, ".", "shot.png"), second}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != first || paths[1] != second {
		t.Fatalf("剪贴板路径归一化错误: %#v", paths)
	}
}

func TestNormalizeFilePathsRejectsUnsafeOrExcessiveInput(t *testing.T) {
	root := t.TempDir()
	for _, paths := range [][]string{{"relative.txt"}, {filepath.Join(root, "bad\x00name")}} {
		if _, err := NormalizeFilePaths(paths, 8); !errors.Is(err, ErrInvalidFilePath) {
			t.Fatalf("不安全路径应被拒绝: paths=%#v err=%v", paths, err)
		}
	}
	if _, err := NormalizeFilePaths([]string{filepath.Join(root, "a"), filepath.Join(root, "b")}, 1); !errors.Is(err, ErrTooManyFiles) {
		t.Fatalf("超量路径应被拒绝: %v", err)
	}
}
