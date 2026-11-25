//go:build darwin || linux

package terminal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 原生工具配置包含完整 Skill 清单时会超过 tmux 的 IPC 消息限制，必须逐字传入。
func TestMultiplexerLongArgumentsRemainLiteralAndReconnect(t *testing.T) {
	mux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("需要 tmux 才能验证真实持久终端")
	}
	root := multiplexerTestRoot(t)
	socket := filepath.Join(root, "t.sock")
	t.Cleanup(func() { _ = exec.Command(mux, "-S", socket, "kill-server").Run() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	launcher, _ := NewPlatformLauncher(PlatformLauncherOptions{})
	var paths strings.Builder
	for range 265 {
		paths.WriteString("- 技能说明与完整路径 => /fixture/skills/personal/example-skill/SKILL.md\n")
	}
	payloads := []string{paths.String(), "含单引号'双引号\"与换行\n下一行\t制表", "$(touch injected) `touch injected2` $HOME ; exit 99", ""}
	outputPath := filepath.Join(root, "args.bin")
	args := []string{"-c", `output=$1; shift; printf '%s\000' "$@" > "$output"; printf 'READY\n'; while IFS= read -r line; do printf 'REPLY:%s\n' "$line"; done`, "fixture", outputPath}
	args = append(args, payloads...)
	// 普通参数禁止控制字符；这里以平台 Launcher 直接验证传输层，保留上层各自的参数门禁。
	request := LaunchRequest{CWD: root, Cols: 80, Rows: 24, Executable: "/bin/sh", Arguments: args, Multiplexer: &MultiplexerSpec{Executable: mux, SocketPath: socket}}
	device, process, err := launcher.Launch(ctx, request)
	if err != nil {
		t.Fatalf("长参数不能被 tmux IPC 拒绝: %v", err)
	}
	t.Cleanup(func() { _ = process.KillTree(); _ = device.Close(); _, _ = process.Wait() })
	want := []byte(strings.Join(payloads, "\x00") + "\x00")
	var got []byte
	for ctx.Err() == nil {
		got, err = os.ReadFile(outputPath)
		if err == nil && len(got) == len(want) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("参数没有逐字传入，got=%d bytes want=%d bytes", len(got), len(want))
	}
	for _, name := range []string{"injected", "injected2"} {
		if _, err := os.Stat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("参数被当作 shell 命令: %s", name)
		}
	}
	assertNoMultiplexerLaunchScripts(t, root)
	pid, err := exec.Command(mux, "-S", socket, "display-message", "-p", "-t", "terminal", "#{pane_pid}").Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := process.(*multiplexedProcess).Detach(); err != nil {
		t.Fatal(err)
	}
	_ = device.Close()
	_, _ = process.Wait()
	request.Multiplexer = &MultiplexerSpec{Executable: mux, SocketPath: socket, ResumeOnly: true}
	reconnected, resumed, err := launcher.Launch(ctx, request)
	if err != nil {
		t.Fatalf("重连必须继续用原始 argv 身份: %v", err)
	}
	t.Cleanup(func() { _ = resumed.KillTree(); _ = reconnected.Close(); _, _ = resumed.Wait() })
	currentPID, err := exec.Command(mux, "-S", socket, "display-message", "-p", "-t", "terminal", "#{pane_pid}").Output()
	if err != nil || string(currentPID) != string(pid) {
		t.Fatalf("重连不得重新启动业务程序: %v", err)
	}
	assertNoMultiplexerLaunchScripts(t, root)
	wrong := request
	wrong.Arguments = append(append([]string(nil), args...), "different")
	if _, _, err := launcher.Launch(ctx, wrong); !errors.Is(err, ErrPathNotGranted) {
		t.Fatalf("不同参数不能复用原会话: %v", err)
	}
}

func multiplexerTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "mzl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func assertNoMultiplexerLaunchScripts(t *testing.T, root string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, ".launch-*.sh"))
	if err != nil || len(files) != 0 {
		t.Fatalf("临时启动参数没有清理: %v (%v)", files, err)
	}
}

func TestMultiplexerLongCommandScriptPermissionsAndCleanup(t *testing.T) {
	root := multiplexerTestRoot(t)
	request := LaunchRequest{Executable: "/bin/sh", Arguments: []string{"-c", "exit 0", strings.Repeat("字", 12000)}}
	command, script, err := prepareMultiplexerCommand(root, request)
	if err != nil || script == nil || len(command) >= multiplexerInlineCommandBytes {
		t.Fatalf("没有为长参数准备短启动命令: %v", err)
	}
	info, err := os.Stat(script.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("启动脚本必须仅当前用户可读写: %v", err)
	}
	script.remove()
	assertNoMultiplexerLaunchScripts(t, root)
	_, script, err = prepareMultiplexerCommand(root, LaunchRequest{Executable: "/bin/true"})
	if err != nil || script != nil {
		t.Fatalf("短命令不应创建临时参数文件: %v", err)
	}
}

func TestMultiplexerLongCommandCleansScriptOnLaunchFailure(t *testing.T) {
	for _, stage := range []string{"new-session", "set-option", "cancel"} {
		t.Run(stage, func(t *testing.T) {
			root := multiplexerTestRoot(t)
			mux := filepath.Join(root, "fake-tmux")
			trace := filepath.Join(root, "commands")
			body := "#!/bin/sh\nprintf '%s\\n' \"$5\" >> " + quoteMultiplexerArgument(trace) + "\ncase \"$5\" in\nhas-session) exit 1;;\nnew-session) "
			body += "for script in " + quoteMultiplexerArgument(root) + "/.launch-*.sh; do test -f \"$script\" && printf 'script-present\\n' >> " + quoteMultiplexerArgument(trace) + "; done; "
			switch stage {
			case "new-session":
				body += "exit 1"
			case "cancel":
				body += "exec sleep 60"
			default:
				body += "exit 0"
			}
			body += ";;\n*) exit 1;;\nesac\n"
			if err := os.WriteFile(mux, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if stage == "cancel" {
				go func() {
					ticker := time.NewTicker(10 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							if calls, err := os.ReadFile(trace); err == nil && strings.Contains(string(calls), "script-present") {
								cancel()
								return
							}
						}
					}
				}()
			}
			launcher, _ := NewPlatformLauncher(PlatformLauncherOptions{})
			_, _, err := launcher.Launch(ctx, LaunchRequest{CWD: root, Cols: 80, Rows: 24, Executable: "/bin/sh", Arguments: []string{"-c", "exit 0", strings.Repeat("x", 32000)}, Multiplexer: &MultiplexerSpec{Executable: mux, SocketPath: filepath.Join(root, "t.sock")}})
			if err == nil {
				t.Fatal("夹具必须拒绝创建会话")
			}
			calls, traceErr := os.ReadFile(trace)
			if traceErr != nil || !strings.Contains(string(calls), "script-present") || stage == "set-option" && !strings.Contains(string(calls), "set-option") {
				t.Fatalf("未到达预期失败阶段: %s / %v / %v", calls, traceErr, err)
			}
			assertNoMultiplexerLaunchScripts(t, root)
		})
	}
}

func TestMultiplexerLongCommandCleanupDoesNotDeleteReplacement(t *testing.T) {
	root := multiplexerTestRoot(t)
	_, script, err := prepareMultiplexerCommand(root, LaunchRequest{Executable: "/bin/true", Arguments: []string{strings.Repeat("x", 32000)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(script.path, script.path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script.path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	script.remove()
	if data, err := os.ReadFile(script.path); err != nil || string(data) != "replacement" {
		t.Fatalf("不能删除同路径下被替换的文件: %v", err)
	}
}

func TestMultiplexerLongCommandCleanupPreservesDetachedLaunch(t *testing.T) {
	for _, detach := range []bool{false, true} {
		t.Run(map[bool]string{false: "exited", true: "detached"}[detach], func(t *testing.T) {
			root := multiplexerTestRoot(t)
			_, script, err := prepareMultiplexerCommand(root, LaunchRequest{Executable: "/bin/true", Arguments: []string{strings.Repeat("x", 32000)}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(script.remove)
			child := newFakeProcess()
			process := &multiplexedProcess{Process: child, launchScript: script}
			if detach {
				if err := process.Detach(); err != nil {
					t.Fatal(err)
				}
			} else {
				child.exit(0, nil)
			}
			_, _ = process.Wait()
			_, err = os.Stat(script.path)
			if detach && err != nil {
				t.Fatalf("后台断开时不能移除原进程尚未读取的参数: %v", err)
			}
			if !detach && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("程序结束后没有兜底清理参数文件: %v", err)
			}
		})
	}
}
