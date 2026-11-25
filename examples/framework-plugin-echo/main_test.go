package main

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/plugin/sdk"
)

func TestDescriptorIsProductNeutralFrameworkPluginExample(t *testing.T) {
	descriptor := pluginDescriptor()
	if descriptor.PluginID != "example.echo" {
		t.Fatalf("示例插件 ID 异常: %#v", descriptor)
	}
	if len(descriptor.Capabilities) != 1 || descriptor.Capabilities[0] != "adapter.browser" {
		t.Fatalf("示例插件必须声明最小 capability: %#v", descriptor.Capabilities)
	}
}

func TestRunServesFrameworkPluginProtocol(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	errOutput := bytes.Buffer{}
	done := make(chan error, 1)
	go func() {
		done <- run(context.Background(), serverReader, serverWriter, &errOutput)
	}()
	client := sdk.NewClient(clientReader, clientWriter)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	handshake, err := client.Handshake(ctx, sdk.HandshakeRequest{
		ProtocolMajor:         1,
		HostID:                "example-test",
		RequestedCapabilities: []string{"adapter.browser"},
	})
	if err != nil {
		t.Fatalf("示例插件握手失败: %v stderr=%s", err, errOutput.String())
	}
	if handshake.PluginID != "example.echo" || len(handshake.GrantedCapabilities) != 1 {
		t.Fatalf("示例插件握手响应异常: %#v", handshake)
	}
	if err := client.Shutdown(ctx, sdk.ShutdownRequest{Reason: "test-complete"}); err != nil {
		t.Fatalf("关闭示例插件失败: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("示例插件应正常退出: %v stderr=%s", err, errOutput.String())
	}
}

func TestRunReportsProtocolErrors(t *testing.T) {
	errOutput := bytes.Buffer{}
	err := run(context.Background(), bytes.NewBufferString("{bad-json}\n"), io.Discard, &errOutput)
	if err == nil {
		t.Fatal("非法协议输入应返回错误")
	}
	if errOutput.Len() == 0 {
		t.Fatal("非法协议输入应写入错误输出")
	}
}
