package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/contract"
	"github.com/heiyeluren/mz-gui/framework/go/adapters/plugin"
	"github.com/heiyeluren/mz-gui/framework/go/adapters/plugin/sdk"
	"github.com/heiyeluren/mz-gui/framework/go/adapters/plugin/sdk/contracttest"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

const (
	showcasePluginChildEnv  = "MZGUI_SHOWCASE_PLUGIN_CHILD"
	showcasePluginID        = "showcase.shell.plugin"
	showcasePluginBuildID   = "showcase-shell"
	showcasePluginChildFlag = "1"
)

func init() {
	if os.Getenv(showcasePluginChildEnv) != showcasePluginChildFlag {
		return
	}
	err := sdk.ServeJSONLines(context.Background(), os.Stdin, os.Stdout, sdk.StaticHandler{
		Descriptor: sdk.Descriptor{
			PluginID:      showcasePluginID,
			ProtocolMajor: 1,
			ProtocolMinor: 0,
			BuildID:       showcasePluginBuildID,
			Capabilities:  []string{"adapter.browser"},
		},
	})
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func probeShowcaseAdapters(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("showcase 适配器上下文无效")
	}
	if err := validateShowcaseAdapter(); err != nil {
		return err
	}
	if _, err := plugin.DecodeManifest([]byte(showcasePluginManifest)); err != nil {
		return fmt.Errorf("解码 showcase 插件 manifest 失败: %w", err)
	}
	guard, err := sdk.NewDegradeGuard(2)
	if err != nil {
		return err
	}
	if err := guard.Allow(); err != nil {
		return err
	}
	guard.Observe(nil)
	return serveShowcasePlugin(ctx)
}

func runShowcasePluginContract(t testing.TB) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("定位 showcase 插件子进程失败: %v", err)
	}
	contracttest.RunBasic(t, contracttest.SuiteOptions{
		Command: contracttest.Command{
			Path: executable,
			Env:  append(os.Environ(), showcasePluginChildEnv+"="+showcasePluginChildFlag),
		},
		Manifest: plugin.Manifest{
			PluginID:              showcasePluginID,
			RequestedCapabilities: []string{"adapter.browser"},
			Process: plugin.Process{
				ProtocolMajor: 1,
				ProtocolMinor: 0,
				BuildID:       showcasePluginBuildID,
			},
		},
		Timeout: 5 * time.Second,
	})
}

func validateShowcaseAdapter() error {
	protocol, err := model.ParseVersion("1.0.0")
	if err != nil {
		return err
	}
	info := contract.AdapterInfo{
		ID:          "showcase.shell",
		DisplayName: "Showcase Shell Adapter",
		BuildID:     "showcase-shell-1",
		Protocol:    protocol,
		Capabilities: []contract.Capability{
			{ID: "adapter.browser", Stability: contract.StabilityCandidate},
		},
	}
	if err := info.Validate(); err != nil {
		return fmt.Errorf("showcase 适配器契约无效: %w", err)
	}
	return nil
}

func serveShowcasePlugin(ctx context.Context) error {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- sdk.ServeJSONLines(ctx, serverReader, serverWriter, sdk.StaticHandler{
			Descriptor: sdk.Descriptor{
				PluginID:      showcasePluginID,
				ProtocolMajor: 1,
				ProtocolMinor: 0,
				BuildID:       showcasePluginBuildID,
				Capabilities:  []string{"adapter.browser"},
			},
		})
	}()
	client := sdk.NewClient(clientReader, clientWriter)
	handshake, err := client.Handshake(ctx, sdk.HandshakeRequest{
		ProtocolMajor:         1,
		HostID:                "showcase-shell",
		RequestedCapabilities: []string{"adapter.browser"},
	})
	if err != nil {
		return fmt.Errorf("showcase 插件握手失败: %w", err)
	}
	if handshake.PluginID != showcasePluginID || handshake.BuildID != showcasePluginBuildID {
		return fmt.Errorf("showcase 插件身份不匹配")
	}
	if err := client.Shutdown(ctx, sdk.ShutdownRequest{Reason: "showcase-shell-probe"}); err != nil {
		return fmt.Errorf("showcase 插件 shutdown 失败: %w", err)
	}
	if err := <-done; err != nil {
		return fmt.Errorf("showcase 插件协议退出异常: %w", err)
	}
	if strings.TrimSpace(handshake.PluginID) == "" {
		return fmt.Errorf("showcase 插件身份为空")
	}
	return nil
}

const showcasePluginManifest = `{"$schema":"./manifest-v1.schema.json","schema_version":1,"plugin_id":"browser.bridge","display_name":"Browser Bridge","publisher":"mz-gui","update_channel":"stable","process":{"protocol_major":1,"protocol_minor":0,"executable_id":"mz-browser-bridge","build_id":"build-20260816","arguments_schema":"schema://browser.bridge/args-v1","health_check":"grpc.health.v1","shutdown_timeout_ms":5000},"requested_capabilities":["adapter.browser","transport.remote-service"],"resource_scopes":[{"kind":"origin","value":"https://example.com"}],"platforms":[{"os":"darwin","arch":"arm64"},{"os":"windows","arch":"amd64"}],"signature":{"key_id":"dev-ed25519","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","signature":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"isolation":{"workdir_mode":"private","env_allowlist":["MZ_PLUGIN_HOME"],"stdio":"framed-json","network":"declared-only"},"limits":{"max_memory_mb":256,"max_message_bytes":262144,"startup_timeout_ms":3000}}`
