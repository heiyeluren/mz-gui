package allowlist

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
)

func TestCheckCommandAndArgRejectUnsafeInput(t *testing.T) {
	if err := CheckCommand("mz-gui-helper"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "../helper", "helper;rm", "C:\\Windows\\cmd.exe", "bad name"} {
		if err := CheckCommand(name); !errors.Is(err, ErrDenied) {
			t.Fatalf("命令 %q 必须拒绝: %v", name, err)
		}
	}
	if err := CheckArg("--format=json"); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"", "a b", "$(id)", "`id`", "ok;ls"} {
		if err := CheckArg(arg); !errors.Is(err, ErrDenied) {
			t.Fatalf("参数 %q 必须拒绝: %v", arg, err)
		}
	}
}

func TestPathPolicyAcceptsRootChildrenAndRejectsEscape(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "data", "file.txt")
	if err := os.MkdirAll(filepath.Dir(inside), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy := PathPolicy{Roots: []string{root}}
	if err := policy.Check(inside); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := policy.Check(outside); !errors.Is(err, ErrDenied) {
		t.Fatalf("越权路径必须拒绝: %v", err)
	}
	if err := (PathPolicy{}).Check(inside); !errors.Is(err, ErrInvalid) {
		t.Fatalf("空 Root 必须无效: %v", err)
	}
}

func TestCommandPolicyAcceptsListedNamesAndRejectsOthers(t *testing.T) {
	policy := CommandPolicy{Names: []string{"mz-gui-helper", "codesign"}}
	if err := policy.Check("mz-gui-helper"); err != nil {
		t.Fatal(err)
	}
	if err := policy.Check("curl"); !errors.Is(err, ErrDenied) {
		t.Fatalf("未登记命令必须拒绝: %v", err)
	}
	if err := policy.Check("../codesign"); !errors.Is(err, ErrDenied) {
		t.Fatalf("格式非法的命令必须拒绝: %v", err)
	}
	if err := (CommandPolicy{}).Check("mz-gui-helper"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("空命令白名单必须无效: %v", err)
	}
	if err := (CommandPolicy{Names: []string{"bad name"}}).Check("mz-gui-helper"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("非法登记名必须使策略无效: %v", err)
	}
}

func TestArgPolicyAcceptsListedValuesAndRejectsOthers(t *testing.T) {
	policy := ArgPolicy{Values: []string{"--format=json", "--verbose"}}
	if err := policy.Check("--format=json"); err != nil {
		t.Fatal(err)
	}
	if err := policy.Check("--output=xml"); !errors.Is(err, ErrDenied) {
		t.Fatalf("未登记参数必须拒绝: %v", err)
	}
	if err := policy.Check("ok;ls"); !errors.Is(err, ErrDenied) {
		t.Fatalf("格式非法的参数必须拒绝: %v", err)
	}
	if err := (ArgPolicy{}).Check("--format=json"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("空参数白名单必须无效: %v", err)
	}
	if err := (ArgPolicy{Values: []string{"a b"}}).Check("--format=json"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("非法登记参数必须使策略无效: %v", err)
	}
}

func TestPathPolicyRejectsControlCharacters(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "ok.txt")
	if err := os.WriteFile(inside, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy := PathPolicy{Roots: []string{root}}
	if err := policy.Check(inside); err != nil {
		t.Fatal(err)
	}
	if err := policy.Check(root + "/ok.txt\x00/secret"); !errors.Is(err, ErrDenied) {
		t.Fatalf("含 NUL 的路径必须拒绝: %v", err)
	}
}

func TestPathPolicyRejectsSymlinkEvenInsideRoot(t *testing.T) {
	testkit.RequireSymlink(t)
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := (PathPolicy{Roots: []string{root}}).Check(link); !errors.Is(err, ErrDenied) {
		t.Fatalf("符号链接必须拒绝: %v", err)
	}
}
