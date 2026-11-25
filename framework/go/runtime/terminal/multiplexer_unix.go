//go:build darwin || linux

package terminal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var multiplexerStartMu sync.Mutex

// tmux 的命令消息有大小上限；为 IPC 包头、工作目录及选项预留空间。
const multiplexerInlineCommandBytes = 8 << 10

type multiplexedProcess struct {
	Process
	spec         MultiplexerSpec
	environment  []string
	created      bool
	launchScript *multiplexerLaunchScript
	detached     atomic.Bool
}

func (p *multiplexedProcess) AbortLaunch() error {
	if p.created {
		return p.KillTree()
	}
	return p.Detach()
}

func multiplexerCommand(ctx context.Context, s MultiplexerSpec, environment []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Executable, append([]string{"-S", s.SocketPath, "-f", "/dev/null"}, args...)...)
	cmd.Env = environment
	// 错误只返回退出状态，不将程序参数或环境写入诊断。
	return cmd.Output()
}

func (p *multiplexedProcess) Detach() error {
	// 后台重启只断开客户端，原程序仍可能尚未读取启动脚本。
	p.detached.Store(true)
	return p.Process.KillTree()
}

func (p *multiplexedProcess) Wait() (int, error) {
	code, err := p.Process.Wait()
	if !p.detached.Load() {
		p.launchScript.remove()
	}
	return code, err
}
func (p *multiplexedProcess) Interrupt() error {
	_, err := multiplexerCommand(context.Background(), p.spec, p.environment, "kill-session", "-t", "terminal")
	if err != nil {
		return errors.New("停止持久终端失败")
	}
	return nil
}
func (p *multiplexedProcess) KillTree() error {
	_ = p.Interrupt()
	err := p.Process.KillTree()
	p.launchScript.remove()
	return err
}

type multiplexerLaunchScript struct {
	path string
	info os.FileInfo
}

// 清理只针对本次创建的普通文件，不删除被替换的文件或链接。
func (s *multiplexerLaunchScript) remove() {
	if s == nil {
		return
	}
	if info, err := os.Lstat(s.path); err == nil && info.Mode().IsRegular() && os.SameFile(info, s.info) {
		_ = os.Remove(s.path)
	}
}

func quoteMultiplexerArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// 长参数只写到当前 socket 的私有父目录；启动 shell 读取后立即删除脚本。
// 参数仍逐个引用，不能把用户文本作为 shell 语法；重连身份始终使用原始 argv。
func prepareMultiplexerCommand(parent string, request LaunchRequest) (string, *multiplexerLaunchScript, error) {
	command := "exec /usr/bin/env -u TMUX -u TMUX_PANE " + quoteMultiplexerArgument(request.Executable)
	for _, argument := range request.Arguments {
		command += " " + quoteMultiplexerArgument(argument)
	}
	if len(command) <= multiplexerInlineCommandBytes {
		return command, nil, nil
	}
	file, err := os.CreateTemp(parent, ".launch-*.sh")
	if err != nil {
		return "", nil, errors.New("准备持久终端启动参数失败")
	}
	script := &multiplexerLaunchScript{path: file.Name()}
	script.info, err = file.Stat()
	if err == nil {
		_, err = file.WriteString("#!/bin/sh\n# 本次启动参数已由 shell 打开，执行前移除临时文件。\n/bin/rm -f -- \"$0\"\n" + command + "\n")
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		script.remove()
		return "", nil, errors.New("准备持久终端启动参数失败")
	}
	return "exec /bin/sh " + quoteMultiplexerArgument(script.path), script, nil
}

func (launcher darwinLauncher) launchMultiplexer(ctx context.Context, request LaunchRequest) (Device, Process, error) {
	s := *request.Multiplexer
	if !validMultiplexer(s) || request.Stdin != nil {
		return nil, nil, ErrInvalidArgument
	}
	parent := filepath.Dir(s.SocketPath)
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil || canonical != parent {
		return nil, nil, ErrPathNotGranted
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, nil, ErrPathNotGranted
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, nil, ErrPathNotGranted
	}
	if st, e := os.Lstat(s.SocketPath); e == nil && st.Mode()&os.ModeSocket == 0 {
		return nil, nil, ErrPathNotGranted
	}
	env := terminalProgramEnvironment(runtime.GOOS, allowedEnvironment(launcher.termProgram, "HOME", "USER", "LOGNAME", "PATH", "LANG", "LC_ALL", "LC_CTYPE", "TMPDIR", "SSH_AUTH_SOCK"), request.Environment)
	identity, _ := json.Marshal([]any{request.CWD, request.Executable, request.Arguments})
	digest := sha256.Sum256(identity)
	owner := hex.EncodeToString(digest[:])
	multiplexerStartMu.Lock()
	defer multiplexerStartMu.Unlock()
	_, err = multiplexerCommand(ctx, s, env, "has-session", "-t", "terminal")
	created := false
	var launchScript *multiplexerLaunchScript
	accepted := false
	defer func() {
		if !accepted {
			launchScript.remove()
		}
	}()
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if s.ResumeOnly {
			return nil, nil, ErrSessionNotFound
		}
		// 清除外层 tmux 标记，程序内部仍可使用自己的独立阶段终端。
		command, script, prepareErr := prepareMultiplexerCommand(parent, request)
		if prepareErr != nil {
			return nil, nil, prepareErr
		}
		launchScript = script
		_, err = multiplexerCommand(ctx, s, env, "new-session", "-d", "-s", "terminal", "-x", strconv.Itoa(request.Cols), "-y", strconv.Itoa(request.Rows), "-c", request.CWD, command)
		if err != nil {
			return nil, nil, fmt.Errorf("创建持久终端失败: %w", err)
		}
		created = true
		for _, option := range [][2]string{{"@terminal-owner", owner}, {"status", "off"}, {"mouse", "on"}, {"history-limit", "10000"}} {
			if _, err = multiplexerCommand(ctx, s, env, "set-option", "-t", "terminal", option[0], option[1]); err != nil {
				_, _ = multiplexerCommand(context.Background(), s, env, "kill-session", "-t", "terminal")
				return nil, nil, errors.New("配置持久终端失败")
			}
		}
	} else {
		value, e := multiplexerCommand(ctx, s, env, "show-options", "-v", "-t", "terminal", "@terminal-owner")
		if e != nil || strings.TrimSpace(string(value)) != owner {
			return nil, nil, ErrPathNotGranted
		}
	}
	if s.PageScrollFallback {
		for _, binding := range [][2]string{
			{"WheelUpPane", "if-shell -F '#{alternate_on}' 'send-keys PPage' 'copy-mode -e'"},
			{"WheelDownPane", "if-shell -F '#{alternate_on}' 'send-keys NPage'"},
		} {
			_, err = multiplexerCommand(ctx, s, env, "bind-key", "-T", "root", binding[0], "if-shell", "-F", "#{||:#{mouse_any_flag},#{pane_in_mode}}", "send-keys -M", binding[1])
			if err != nil {
				if created {
					_, _ = multiplexerCommand(context.Background(), s, env, "kill-session", "-t", "terminal")
				}
				return nil, nil, errors.New("配置终端翻页失败")
			}
		}
	}
	attach := request
	attach.Multiplexer = nil
	attach.Executable = s.Executable
	attach.Arguments = []string{"-S", s.SocketPath, "-f", "/dev/null", "attach-session", "-t", "terminal"}
	device, process, err := launcher.Launch(ctx, attach)
	if err != nil {
		if created {
			_, _ = multiplexerCommand(context.Background(), s, env, "kill-session", "-t", "terminal")
		}
		return nil, nil, err
	}
	accepted = true
	return device, &multiplexedProcess{Process: process, spec: s, environment: env, created: created, launchScript: launchScript}, nil
}
