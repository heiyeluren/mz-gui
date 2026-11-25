package filegrant

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPolicyValidatesAndCanonicalizesDirectoryGrant(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	canonical, err := testPolicy().ValidateCreate(CreateRequest{
		Subject: "desktop", Path: root, Kind: KindDirectory, Access: AccessReadWrite,
		Source: "native_dialog", Now: now,
	}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if canonical != want {
		t.Fatalf("规范路径 = %q，期望 %q", canonical, want)
	}
}

func TestPolicyRejectsSubjectsSourcesAccessAndExpirationOutsidePolicy(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "note.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	base := CreateRequest{
		Subject: "desktop", Path: filePath, Kind: KindFile, Access: AccessRead,
		Source: "native_dialog", ExpiresAt: &expires, Now: now,
	}

	tests := []struct {
		name   string
		mutate func(*CreateRequest)
	}{
		{name: "主体", mutate: func(request *CreateRequest) { request.Subject = "browser" }},
		{name: "来源", mutate: func(request *CreateRequest) { request.Source = "manual" }},
		{name: "文件写权限", mutate: func(request *CreateRequest) { request.Access = AccessReadWrite }},
		{name: "文件缺少有效期", mutate: func(request *CreateRequest) { request.ExpiresAt = nil }},
		{name: "超过最长有效期", mutate: func(request *CreateRequest) { value := now.Add(8 * 24 * time.Hour); request.ExpiresAt = &value }},
		{name: "空幂等键", mutate: func(request *CreateRequest) {}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			test.mutate(&request)
			key := "create-1"
			if test.name == "空幂等键" {
				key = ""
			}
			if _, err := testPolicy().ValidateCreate(request, key); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("错误 = %v，期望 ErrInvalidArgument", err)
			}
		})
	}
}

func TestMapPathErrorMapsPermissionDeniedAndTCCWithoutErrnoLeak(t *testing.T) {
	cases := []error{
		os.ErrPermission,
		&os.PathError{Op: "lstat", Path: "/Users/secret/Documents", Err: os.ErrPermission},
		errors.New("lstat /Users/secret/Documents: permission denied"),
		errors.New("open /Users/secret/Documents: operation not permitted"),
		errors.New("stat /Users/secret/Library/Mail: EPERM"),
		errors.New("readdir /Users/secret/Desktop: EACCES"),
		errors.New("TCC denied kTCCServiceSystemPolicyDocumentsFolder"),
	}
	for _, err := range cases {
		got := MapPathError(err)
		if !errors.Is(got, ErrPermissionDenied) {
			t.Fatalf("错误 %v 应映射 ErrPermissionDenied，实际 %v", err, got)
		}
		if got.Error() != "系统未授权访问该目录" {
			t.Fatalf("中文文案 = %q", got.Error())
		}
		assertNoErrnoLeak(t, got.Error())
	}
	if got := MapPathError(os.ErrNotExist); !errors.Is(got, ErrInvalidArgument) {
		t.Fatalf("其他路径错误应保持 ErrInvalidArgument: %v", got)
	}
}

func TestValidatePathMapsUnreadableDirectoryToPermissionDenied(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "denied")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	_, err := ValidatePath(child, KindDirectory)
	if err == nil {
		t.Skip("当前用户仍可读取该目录，跳过真实权限拒绝用例")
	}
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("不可读目录错误 = %v，期望 ErrPermissionDenied", err)
	}
	assertNoErrnoLeak(t, err.Error())
}

func assertNoErrnoLeak(t *testing.T, message string) {
	t.Helper()
	leaks := []string{"permission denied", "operation not permitted", "EACCES", "EPERM", "eacces", "eperm", "errno"}
	for _, leak := range leaks {
		if strings.Contains(message, leak) {
			t.Fatalf("用户可见原因泄漏英文 errno %q: %q", leak, message)
		}
	}
}

func TestValidatePathRejectsRelativeSymlinkAndKindMismatch(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "note.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidatePath("relative/path", KindDirectory); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("相对路径错误 = %v", err)
	}
	if _, err := ValidatePath(filePath, Kind("unknown")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("未知类型错误 = %v", err)
	}
	if _, err := ValidatePath(root, KindFile); !errors.Is(err, ErrKindMismatch) {
		t.Fatalf("类型不匹配错误 = %v", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err == nil {
		if _, err := ValidatePath(link, KindDirectory); !errors.Is(err, ErrSymbolicLink) {
			t.Fatalf("符号链接错误 = %v", err)
		}
	}
}

func TestGrantActivityCapabilityAndScopeRoundTrip(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	grant := Grant{ExpiresAt: &expires}
	if !grant.Active(now) || grant.Active(expires) {
		t.Fatal("授权有效状态计算错误")
	}
	if got := Capability(KindDirectory, AccessReadWrite); got != "filesystem.directory.read_write" {
		t.Fatalf("capability = %q", got)
	}

	encoded, err := EncodeScope(Scope{SchemaVersion: 1, Type: "filesystem", Path: "/tmp/example", Kind: KindDirectory, Access: AccessRead})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeScope(encoded)
	if err != nil || decoded.Path != "/tmp/example" || decoded.Kind != KindDirectory {
		t.Fatalf("scope 往返错误: scope=%#v err=%v", decoded, err)
	}
	if _, err := DecodeScope([]byte(`{"schema_version":2,"type":"filesystem"}`)); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("无效 scope 错误 = %v", err)
	}
}

func testPolicy() Policy {
	return Policy{
		AllowedSubjects: []string{"desktop"},
		AllowedSources:  []string{"native_dialog"},
		MaxPathLength:   4096,
		MaxLifetime:     7 * 24 * time.Hour,
		PersistentKinds: []Kind{KindDirectory},
		AccessByKind: map[Kind][]Access{
			KindFile:      {AccessRead},
			KindDirectory: {AccessRead, AccessReadWrite},
		},
	}
}
