package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
)

func TestStoreCreatesPrivateInitialDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "settings.data")
	store := New(Options{Path: path, MaxBytes: 128, Initial: []byte("version=1\n")})

	snapshot, err := store.LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if string(snapshot.Data) != "version=1\n" || !strings.HasPrefix(snapshot.Revision, "sha256:") {
		t.Fatalf("首次快照错误: %#v", snapshot)
	}
	testkit.AssertFileMode(t, path, 0o600)
}

func TestStoreRejectsOversizedAndSymbolicLinkDocuments(t *testing.T) {
	testkit.RequireSymlink(t)
	root := t.TempDir()
	oversized := filepath.Join(root, "oversized.data")
	if err := os.WriteFile(oversized, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{Path: oversized, MaxBytes: 4}).LoadOrCreate(); !errors.Is(err, ErrDocumentTooLarge) {
		t.Fatalf("超大文档错误 = %v", err)
	}

	target := filepath.Join(root, "target.data")
	if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.data")
	if err := os.Symlink(target, link); err == nil {
		if _, err := New(Options{Path: link, MaxBytes: 16}).LoadOrCreate(); !errors.Is(err, ErrSymbolicLink) {
			t.Fatalf("符号链接错误 = %v", err)
		}
	}
}

func TestStoreUpdateUsesRevisionAndBacksUpOriginal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "settings.data")
	backupPath := filepath.Join(root, "history", "before.data")
	store := New(Options{
		Path: path, MaxBytes: 128, Initial: []byte("version=1\n"),
		BackupPath: func(string, string) string { return backupPath },
	})
	initial, err := store.LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(initial.Revision, []byte("version=2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision == initial.Revision || string(updated.Data) != "version=2\n" {
		t.Fatalf("更新快照错误: %#v", updated)
	}
	backup, err := os.ReadFile(backupPath)
	if err != nil || string(backup) != "version=1\n" {
		t.Fatalf("原内容备份错误: data=%q err=%v", backup, err)
	}
	if _, err := store.Update(initial.Revision, []byte("version=3\n")); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("旧 revision 错误 = %v", err)
	}
	unchanged, err := store.Update(updated.Revision, append([]byte(nil), updated.Data...))
	if err != nil || unchanged.Revision != updated.Revision {
		t.Fatalf("无变化更新错误: snapshot=%#v err=%v", unchanged, err)
	}
}

func TestStoreUpdateEnforcesSchemaAndLockedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	registry := NewRegistry()
	if err := registry.Register(Schema{
		ID: "app.settings", Version: 1,
		Validate: func(data []byte) error {
			if !strings.Contains(string(data), "schema_version = 1") {
				return ErrInvalidSchema
			}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	initialDoc := []byte("schema_version = 1\n\n[security]\nmode = \"strict\"\n\n[ui]\ntheme = \"light\"\n")
	store := New(Options{
		Path: path, MaxBytes: 1024, Initial: initialDoc,
		Registry: registry, SchemaID: "app.settings", SchemaVersion: 1,
		LockedKeys: []string{"security.mode"},
	})
	initial, err := store.LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(initial.Revision, []byte("schema_version = 2\n\n[security]\nmode = \"strict\"\n")); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("非法 schema 必须拒绝: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(initialDoc) {
		t.Fatalf("非法 schema 不得改写文件: %q err=%v", current, err)
	}
	if _, err := store.Update(initial.Revision, []byte("schema_version = 1\n\n[security]\nmode = \"loose\"\n")); !errors.Is(err, ErrPolicyLocked) {
		t.Fatalf("锁定字段必须拒绝: %v", err)
	}
	updated, err := store.Update(initial.Revision, []byte("schema_version = 1\n\n[security]\nmode = \"strict\"\n\n[ui]\ntheme = \"dark\"\n"))
	if err != nil || !strings.Contains(string(updated.Data), "theme = \"dark\"") {
		t.Fatalf("未锁定字段应允许更新: snapshot=%#v err=%v", updated, err)
	}
}

func TestStoreRejectsInvalidOptionsAndOversizedCandidate(t *testing.T) {
	if _, err := New(Options{}).LoadOrCreate(); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("空路径错误 = %v", err)
	}
	path := filepath.Join(t.TempDir(), "settings.data")
	store := New(Options{Path: path, MaxBytes: 4, Initial: []byte("1234")})
	initial, err := store.LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(initial.Revision, []byte("12345")); !errors.Is(err, ErrDocumentTooLarge) {
		t.Fatalf("超大候选错误 = %v", err)
	}
}

func TestLoadExistingReadsPrivateDocumentWithoutCreatingMissingPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "legacy.toml")
	if _, err := LoadExisting(path, 128); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("缺失文档错误 = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("安全读取不得创建缺失文档: %v", err)
	}
	if err := os.WriteFile(path, []byte("schema_version=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := LoadExisting(path, 128)
	if err != nil || string(snapshot.Data) != "schema_version=1\n" || snapshot.Revision != Digest(snapshot.Data) {
		t.Fatalf("读取现有文档失败: snapshot=%#v err=%v", snapshot, err)
	}
}

func TestStoreRejectsUnsafeOrConflictingExistingBackup(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(t *testing.T, path string)
		wantError error
	}{
		{
			name: "符号链接",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				testkit.RequireSymlink(t)
				target := filepath.Join(filepath.Dir(path), "target.data")
				if err := os.WriteFile(target, []byte("version=1\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			},
			wantError: ErrSymbolicLink,
		},
		{
			name: "摘要冲突",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("unrelated\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: ErrBackupConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "settings.data")
			backupPath := filepath.Join(root, "backup.data")
			store := New(Options{
				Path: path, MaxBytes: 128, Initial: []byte("version=1\n"),
				BackupPath: func(string, string) string { return backupPath },
			})
			initial, err := store.LoadOrCreate()
			if err != nil {
				t.Fatal(err)
			}
			test.prepare(t, backupPath)
			if _, err := store.Update(initial.Revision, []byte("version=2\n")); !errors.Is(err, test.wantError) {
				t.Fatalf("错误 = %v，期望 %v", err, test.wantError)
			}
		})
	}
}
