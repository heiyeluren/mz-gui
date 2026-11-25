package gitstatus

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestParsePorcelainV2BranchAndChanges(t *testing.T) {
	snapshot, err := parsePorcelainV2([]byte("# branch.oid abc123\n# branch.head feature/test\n# branch.ab +2 -1\n1 M. N... 100644 100644 100644 a b staged.txt\n1 .M N... 100644 100644 100644 a b modified.txt\n? untracked.txt\nu UU N... 100644 100644 100644 100644 a b c conflict.txt\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.IsRepository || snapshot.Branch != "feature/test" || snapshot.Ahead != 2 || snapshot.Behind != 1 {
		t.Fatalf("分支摘要错误: %#v", snapshot)
	}
	if snapshot.Staged != 1 || snapshot.Modified != 1 || snapshot.Untracked != 1 || snapshot.Conflicted != 1 || snapshot.Clean {
		t.Fatalf("变更计数错误: %#v", snapshot)
	}
}

func TestInspectorReadsRepositoryWithoutShell(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("当前环境没有 git")
	}
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.email", "mz-gui@example.invalid")
	runGit(t, root, "config", "user.name", "MZ GUI Test")
	tracked := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(tracked, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	inspector := New(Config{Timeout: 2 * time.Second})
	snapshot, err := inspector.Inspect(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Branch != "main" || snapshot.Modified != 1 || snapshot.Untracked != 1 || snapshot.Clean {
		t.Fatalf("仓库状态错误: %#v", snapshot)
	}
}

func TestInspectorReturnsNonRepositoryWithoutLeakingCommandOutput(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("当前环境没有 git")
	}
	inspector := New(Config{Timeout: 2 * time.Second})
	snapshot, err := inspector.Inspect(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.IsRepository || snapshot.StatusCode != "not_repository" {
		t.Fatalf("普通目录状态错误: %#v", snapshot)
	}
}

func TestInspectorRejectsRelativeAndSymlinkDirectories(t *testing.T) {
	inspector := New(Config{})
	if _, err := inspector.Inspect(context.Background(), "relative/path"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("相对路径必须被拒绝，实际错误: %v", err)
	}

	realDirectory := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked-project")
	if err := os.Symlink(realDirectory, link); err != nil {
		t.Skipf("当前平台不能创建目录软链接: %v", err)
	}
	if _, err := inspector.Inspect(context.Background(), link); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("目录软链接必须被拒绝，实际错误: %v", err)
	}
}

func TestInspectorReportsUnavailableExecutable(t *testing.T) {
	inspector := New(Config{Executable: "mz-gui-git-command-that-does-not-exist"})
	if _, err := inspector.Inspect(context.Background(), t.TempDir()); !errors.Is(err, ErrGitUnavailable) {
		t.Fatalf("缺少 Git 可执行文件时错误不正确: %v", err)
	}
}

func TestInspectorHonorsCanceledContext(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("当前环境没有 git")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(Config{}).Inspect(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消上下文必须向上传递，实际错误: %v", err)
	}
}

func TestParsePorcelainV2DetachedAndMalformedRecords(t *testing.T) {
	snapshot, err := parsePorcelainV2([]byte("# branch.oid abc123\n# branch.head (detached)\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Detached || snapshot.Branch != "HEAD" || !snapshot.Clean {
		t.Fatalf("游离 HEAD 解析错误: %#v", snapshot)
	}

	for _, value := range [][]byte{
		[]byte("# branch.ab +x -1\n"),
		[]byte("1 M malformed\n"),
		[]byte("unexpected record\n"),
	} {
		if _, err := parsePorcelainV2(value); !errors.Is(err, ErrMalformed) {
			t.Fatalf("畸形状态必须被拒绝，输入 %q，实际错误: %v", value, err)
		}
	}
}

func TestLimitedBufferRejectsOversizedOutput(t *testing.T) {
	var buffer limitedBuffer
	if _, err := buffer.Write(make([]byte, maxOutputBytes+1)); !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("超限输出必须被拒绝，实际错误: %v", err)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v 失败: %v: %s", args, err, output)
	}
}
