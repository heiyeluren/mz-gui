package sdk

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestStaticHandlerNegotiatesHandshakeAndCapabilities(t *testing.T) {
	handler := StaticHandler{
		Descriptor: Descriptor{
			PluginID:      "example.echo",
			ProtocolMajor: 1,
			ProtocolMinor: 0,
			BuildID:       "test-build",
			Capabilities:  []string{"adapter.browser", "transport.remote-service"},
		},
	}
	response, err := handler.Handshake(context.Background(), HandshakeRequest{
		ProtocolMajor:         1,
		HostID:                "contract-suite",
		RequestedCapabilities: []string{"adapter.browser", "adapter.unknown"},
	})
	if err != nil {
		t.Fatalf("握手不应失败: %v", err)
	}
	if response.PluginID != "example.echo" || response.ProtocolMajor != 1 {
		t.Fatalf("握手响应身份异常: %#v", response)
	}
	if len(response.GrantedCapabilities) != 1 || response.GrantedCapabilities[0] != "adapter.browser" {
		t.Fatalf("只应授权插件声明的 capability: %#v", response.GrantedCapabilities)
	}
	allowed, err := handler.CheckCapability(context.Background(), CapabilityRequest{Capability: "adapter.browser"})
	if err != nil || !allowed.Allowed {
		t.Fatalf("已声明 capability 应允许: allowed=%#v err=%v", allowed, err)
	}
	denied, err := handler.CheckCapability(context.Background(), CapabilityRequest{Capability: "adapter.unknown"})
	if err != nil || denied.Allowed || denied.Reason != "unsupported" {
		t.Fatalf("未声明 capability 应明确拒绝: denied=%#v err=%v", denied, err)
	}
}

func TestClientServerJSONLineProtocol(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	handler := StaticHandler{
		Descriptor: Descriptor{
			PluginID:      "example.echo",
			ProtocolMajor: 1,
			ProtocolMinor: 0,
			BuildID:       "test-build",
			Capabilities:  []string{"adapter.browser"},
		},
	}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- ServeJSONLines(context.Background(), serverReader, serverWriter, handler)
	}()
	client := NewClient(clientReader, clientWriter)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	handshake, err := client.Handshake(ctx, HandshakeRequest{
		ProtocolMajor:         1,
		HostID:                "contract-suite",
		RequestedCapabilities: []string{"adapter.browser"},
	})
	if err != nil {
		t.Fatalf("客户端握手失败: %v", err)
	}
	if handshake.PluginID != "example.echo" || len(handshake.GrantedCapabilities) != 1 {
		t.Fatalf("握手响应异常: %#v", handshake)
	}
	capability, err := client.CheckCapability(ctx, CapabilityRequest{Capability: "adapter.browser"})
	if err != nil || !capability.Allowed {
		t.Fatalf("capability 检查失败: capability=%#v err=%v", capability, err)
	}
	if err := client.Shutdown(ctx, ShutdownRequest{Reason: "test-complete"}); err != nil {
		t.Fatalf("shutdown 失败: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server 应正常退出: %v", err)
	}
}
