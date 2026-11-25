package fileopener

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentRejectsEmptyRelativeAndNonAbsolutePaths(t *testing.T) {
	var opener Current
	for _, path := range []string{"", " ", "readme.md", "./local.txt", "../escape", "relative/path"} {
		if err := opener.Open(path); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("路径 %q 必须拒绝: %v", path, err)
		}
	}
}

func TestValidExternalURLAcceptsOnlyHTTPSWithoutCredentialsOrFragment(t *testing.T) {
	for _, value := range []string{"https://example.com/oauth?state=abc", " https://auth.openai.com/oauth/authorize?state=abc "} {
		if !validExternalURL(value) {
			t.Fatalf("应接受安全 HTTPS URL: %q", value)
		}
	}
	for _, value := range []string{"", "http://example.com", "https://user:pass@example.com", "https://example.com/#token"} {
		if validExternalURL(value) {
			t.Fatalf("不应接受不安全 URL: %q", value)
		}
	}
}

func TestOpenDirectoryRejectsFilesAndMissingDirectories(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "document")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"relative", path} {
		if err := (Current{}).OpenDirectory(target); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("必须拒绝非绝对目录 %q: %v", target, err)
		}
	}
	if err := (Current{}).OpenDirectory(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("目录不存在应返回原因: %v", err)
	}
}
