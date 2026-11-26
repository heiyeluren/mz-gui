package filedrop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
)

func TestNormalizeRegularFilesAcceptsAbsoluteRegularFilesAndDeduplicates(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.png")
	second := filepath.Join(directory, "second.txt")
	if err := os.WriteFile(first, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(directory, "first-alias.png")
	if err := os.Link(first, alias); err != nil {
		t.Fatal(err)
	}
	values, err := NormalizeRegularFiles([]string{first, first, alias, second}, 8)
	if err != nil || len(values) != 2 || values[0] != first || values[1] != second {
		t.Fatalf("原生拖放文件规范化错误: values=%v err=%v", values, err)
	}
}

func TestNormalizeRegularFilesRejectsRelativeDirectorySymlinkAndOverflow(t *testing.T) {
	testkit.RequireSymlink(t)
	directory := t.TempDir()
	file := filepath.Join(directory, "file.png")
	if err := os.WriteFile(file, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link.png")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		paths []string
		want  error
	}{
		{name: "相对路径", paths: []string{"file.png"}, want: ErrInvalidFile},
		{name: "目录", paths: []string{directory}, want: ErrInvalidFile},
		{name: "符号链接", paths: []string{link}, want: ErrInvalidFile},
		{name: "超量", paths: []string{file, filepath.Join(directory, "other.png")}, want: ErrTooManyFiles},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NormalizeRegularFiles(testCase.paths, 1); !errors.Is(err, testCase.want) {
				t.Fatalf("错误=%v，期望=%v", err, testCase.want)
			}
		})
	}
}
