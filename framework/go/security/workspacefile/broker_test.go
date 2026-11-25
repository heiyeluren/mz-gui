package workspacefile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBrokerReadsRegularFileInsideAuthorizedRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "src", "main.go")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("package main\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	broker := newTestBroker(t, 1024)

	result, err := broker.ReadFile(context.Background(), root, "src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Content) != string(content) || result.SizeBytes != int64(len(content)) || result.SHA256 != digest(content) {
		t.Fatalf("读取结果不完整: %#v", result)
	}
}

func TestBrokerRejectsPathEscapeAbsoluteAndSymbolicLinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	broker := newTestBroker(t, 1024)

	for _, relative := range []string{"../outside.txt", "/etc/passwd", `C:\\Windows\\win.ini`, "nested/../../outside.txt", "CON"} {
		if _, err := broker.ReadFile(context.Background(), root, relative); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("不安全路径 %q 的错误 = %v", relative, err)
		}
	}

	link := filepath.Join(root, "linked.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前文件系统不支持符号链接: %v", err)
	}
	if _, err := broker.ReadFile(context.Background(), root, "linked.txt"); !errors.Is(err, ErrSymbolicLink) {
		t.Fatalf("文件符号链接错误 = %v", err)
	}
	if _, err := broker.WriteFile(context.Background(), root, "linked.txt", []byte("blocked"), digest([]byte("secret"))); !errors.Is(err, ErrSymbolicLink) {
		t.Fatalf("写入文件符号链接错误 = %v", err)
	}

	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDirectory, "value.txt"), []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedDirectory := filepath.Join(root, "linked-dir")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Skipf("当前文件系统不支持目录符号链接: %v", err)
	}
	if _, err := broker.ReadFile(context.Background(), root, "linked-dir/value.txt"); !errors.Is(err, ErrSymbolicLink) {
		t.Fatalf("目录符号链接错误 = %v", err)
	}
}

func TestBrokerRejectsOversizedReadAndWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	broker := newTestBroker(t, 4)

	if _, err := broker.ReadFile(context.Background(), root, "large.txt"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("超限读取错误 = %v", err)
	}
	if _, err := broker.WriteFile(context.Background(), root, "new.txt", []byte("12345"), ""); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("超限写入错误 = %v", err)
	}
}

func TestBrokerWritesWithDigestConflictProtection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	current := []byte("version one\n")
	if err := os.WriteFile(path, current, 0o640); err != nil {
		t.Fatal(err)
	}
	broker := newTestBroker(t, 1024)

	if _, err := broker.WriteFile(context.Background(), root, "README.md", []byte("version two\n"), digest([]byte("stale"))); !errors.Is(err, ErrConflict) {
		t.Fatalf("过期摘要错误 = %v", err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(current) {
		t.Fatalf("冲突后文件被修改: %q err=%v", unchanged, err)
	}

	next := []byte("version two\n")
	result, err := broker.WriteFile(context.Background(), root, "README.md", next, digest(current))
	if err != nil {
		t.Fatal(err)
	}
	if result.Created || result.SHA256 != digest(next) || result.SizeBytes != int64(len(next)) {
		t.Fatalf("覆盖结果错误: %#v", result)
	}
	written, err := os.ReadFile(path)
	if err != nil || string(written) != string(next) {
		t.Fatalf("覆盖内容错误: %q err=%v", written, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("覆盖后权限未保留: mode=%v err=%v", info.Mode(), err)
	}
}

func TestBrokerCreatesNewFileAndRejectsBlindOverwrite(t *testing.T) {
	root := t.TempDir()
	broker := newTestBroker(t, 1024)
	content := []byte("new file\n")

	result, err := broker.WriteFile(context.Background(), root, "new.txt", content, "")
	if err != nil || !result.Created || result.SHA256 != digest(content) {
		t.Fatalf("新建文件失败: result=%#v err=%v", result, err)
	}
	if _, err := broker.WriteFile(context.Background(), root, "new.txt", []byte("overwrite"), ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("无摘要覆盖错误 = %v", err)
	}
}

func TestBrokerSerializesConcurrentDigestWrites(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "shared.txt")
	current := []byte("current\n")
	if err := os.WriteFile(path, current, 0o600); err != nil {
		t.Fatal(err)
	}
	broker := newTestBroker(t, 1024)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, content := range [][]byte{[]byte("first\n"), []byte("second\n")} {
		content := content
		go func() {
			<-start
			_, err := broker.WriteFile(context.Background(), root, "shared.txt", content, digest(current))
			results <- err
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("并发保存返回意外错误: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("并发保存结果错误: success=%d conflict=%d", successes, conflicts)
	}
}

func TestBrokerHonorsCancelledContextAndRejectsUnsafeRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "value.txt"), []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	broker := newTestBroker(t, 1024)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := broker.ReadFile(ctx, root, "value.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}

	realRoot := t.TempDir()
	linkedRoot := filepath.Join(t.TempDir(), "linked-root")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		t.Skipf("当前文件系统不支持符号链接: %v", err)
	}
	if _, err := broker.WriteFile(context.Background(), linkedRoot, "value.txt", []byte("value"), ""); !errors.Is(err, ErrSymbolicLink) {
		t.Fatalf("符号链接根目录错误 = %v", err)
	}
}

func newTestBroker(t *testing.T, maxBytes int64) *Broker {
	t.Helper()
	broker, err := New(Options{MaxReadBytes: maxBytes, MaxWriteBytes: maxBytes})
	if err != nil {
		t.Fatal(err)
	}
	return broker
}

func digest(content []byte) string {
	value := sha256.Sum256(content)
	return hex.EncodeToString(value[:])
}
