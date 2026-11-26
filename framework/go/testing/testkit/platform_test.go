package testkit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssertFileModeUsesHostSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.data")
	if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	AssertFileMode(t, path, 0o600)
}

func TestRequireSymlinkUsesCurrentAccountCapability(t *testing.T) {
	RequireSymlink(t)
}
