// Package contracttest 提供 Framework 插件 SDK 的跨进程契约测试工具。
package contracttest

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/plugin"
	"github.com/heiyeluren/mz-gui/framework/go/adapters/plugin/sdk"
)

type Command struct {
	Path string
	Args []string
	Env  []string
	Dir  string
}

type SuiteOptions struct {
	Command  Command
	Manifest plugin.Manifest
	Timeout  time.Duration
}

func RunBasic(t testing.TB, options SuiteOptions) {
	t.Helper()
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, options.Command.Path, options.Command.Args...)
	cmd.Env = options.Command.Env
	cmd.Dir = options.Command.Dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("创建插件 stdin 失败: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("创建插件 stdout 失败: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动插件进程失败: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()
	client := sdk.NewClient(stdout, stdin)
	handshake, err := client.Handshake(ctx, sdk.HandshakeRequest{
		ProtocolMajor:         options.Manifest.Process.ProtocolMajor,
		HostID:                "mz-gui-contract-suite",
		RequestedCapabilities: options.Manifest.RequestedCapabilities,
	})
	if err != nil {
		t.Fatalf("插件握手失败: %v stderr=%s", err, stderr.String())
	}
	if handshake.PluginID != options.Manifest.PluginID ||
		handshake.ProtocolMajor != options.Manifest.Process.ProtocolMajor ||
		handshake.BuildID != options.Manifest.Process.BuildID {
		t.Fatalf("插件身份不匹配: handshake=%#v manifest=%#v", handshake, options.Manifest)
	}
	for _, capability := range options.Manifest.RequestedCapabilities {
		response, err := client.CheckCapability(ctx, sdk.CapabilityRequest{Capability: capability})
		if err != nil || !response.Allowed {
			t.Fatalf("声明 capability 未通过: capability=%s response=%#v err=%v", capability, response, err)
		}
	}
	denied, err := client.CheckCapability(ctx, sdk.CapabilityRequest{Capability: "adapter.unregistered"})
	if err != nil || denied.Allowed || denied.Reason == "" {
		t.Fatalf("未知 capability 应明确拒绝: denied=%#v err=%v", denied, err)
	}
	if err := client.Shutdown(ctx, sdk.ShutdownRequest{Reason: "contract-suite-complete"}); err != nil {
		t.Fatalf("插件 shutdown 失败: %v stderr=%s", err, stderr.String())
	}
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("插件退出异常: %v stderr=%s", err, stderr.String())
		}
	case <-ctx.Done():
		t.Fatalf("插件未在期限内退出: %v stderr=%s", ctx.Err(), stderr.String())
	}
}

func (command Command) String() string {
	return fmt.Sprintf("%s %v", command.Path, command.Args)
}
