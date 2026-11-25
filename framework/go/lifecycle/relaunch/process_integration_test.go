package relaunch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// 测试二进制在后继进程中扮演 GUI 入口；等待行为与产品使用同一公共入口。
func init() {
	if len(os.Args) > 1 && os.Args[1] == WaitFlag && os.Getenv("MZ_RELAUNCH_TEST_ROLE") == "child" {
		if err := AwaitParent(context.Background(), os.Args[1:]); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Getenv("MZ_RELAUNCH_TEST_MARKER"), []byte(os.Getenv("MZ_TEST_ROOT")+"|"+os.Getenv("MZ_TEST_CACHE")), 0600); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
}

func TestRelaunchParentProcess(t *testing.T) {
	if os.Getenv("MZ_RELAUNCH_TEST_ROLE") != "parent" {
		return
	}
	_, err := Start(context.Background(), map[string]string{"MZ_RELAUNCH_TEST_ROLE": "child", "MZ_TEST_ROOT": "target-home", "MZ_TEST_CACHE": "target-cache"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if _, err := os.Stat(os.Getenv("MZ_RELAUNCH_TEST_MARKER")); !os.IsNotExist(err) {
		t.Fatal("旧父进程尚未退出，后继已开始运行")
	}
}

func TestRelaunchActuallyWaitsForParentAndKeepsEnvironment(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRelaunchParentProcess$")
	command.Env = environment(os.Environ(), map[string]string{"MZ_RELAUNCH_TEST_ROLE": "parent", "MZ_RELAUNCH_TEST_MARKER": marker})
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("父进程失败: %v %s", err, output)
	}
	for {
		data, err := os.ReadFile(marker)
		if err == nil {
			if string(data) != "target-home|target-cache" {
				t.Fatalf("环境传递错误: %s", data)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("旧进程退出后没有启动后继")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
