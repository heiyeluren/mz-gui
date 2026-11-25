package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecodeManifestRejectsUnsafePluginManifest(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "未知字段", data: strings.Replace(validManifestJSON(), `"schema_version":1`, `"schema_version":1,"unknown":true`, 1)},
		{name: "可执行文件不能是路径", data: strings.Replace(validManifestJSON(), `"executable_id":"mz-browser-bridge"`, `"executable_id":"../mz-browser-bridge"`, 1)},
		{name: "缺少签名", data: strings.Replace(validManifestJSON(), `"signature":"`+signature()+`"`, `"signature":""`, 1)},
		{name: "缺少隔离策略", data: strings.Replace(validManifestJSON(), `"isolation":{"workdir_mode":"private","env_allowlist":["MZ_PLUGIN_HOME"],"stdio":"framed-json","network":"declared-only"},`, ``, 1)},
		{name: "能力重复", data: strings.Replace(validManifestJSON(), `"requested_capabilities":["adapter.browser","transport.remote-service"]`, `"requested_capabilities":["adapter.browser","adapter.browser"]`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeManifest([]byte(test.data)); !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("应拒绝非法插件 manifest: %v", err)
			}
		})
	}
}

func TestDecodeManifestAcceptsIsolatedSignedManifest(t *testing.T) {
	manifest, err := DecodeManifest([]byte(validManifestJSON()))
	if err != nil {
		t.Fatalf("有效插件 manifest 不应失败: %v", err)
	}
	if manifest.PluginID != "browser.bridge" || manifest.Process.ProtocolMajor != 1 {
		t.Fatalf("manifest 解码异常: %#v", manifest)
	}
	if len(manifest.RequestedCapabilities) != 2 || len(manifest.Platforms) != 2 {
		t.Fatalf("manifest 能力或平台异常: %#v", manifest)
	}
	if manifest.ShutdownTimeout != 5*time.Second {
		t.Fatalf("shutdown timeout 异常: %s", manifest.ShutdownTimeout)
	}
}

func TestManifestBuildsManagedProcessSpecWithoutLeakingSecrets(t *testing.T) {
	manifest, err := DecodeManifest([]byte(validManifestJSON()))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := manifest.ManagedProcessSpec(LaunchOptions{
		ExecutablePath: "/private/plugins/mz-browser-bridge",
		WorkDir:        "/private/plugins/work/browser.bridge",
		Environment: map[string]string{
			"MZ_PLUGIN_HOME": "/private/plugins/work/browser.bridge",
			"SECRET_TOKEN":   "must-not-pass",
		},
	})
	if err != nil {
		t.Fatalf("生成受管进程 spec 失败: %v", err)
	}
	if spec.ID != "plugin.browser.bridge" || spec.Executable != "/private/plugins/mz-browser-bridge" {
		t.Fatalf("受管进程 spec 异常: %#v", spec)
	}
	if _, ok := spec.Environment["SECRET_TOKEN"]; ok {
		t.Fatalf("未声明环境变量不应进入插件进程: %#v", spec.Environment)
	}
	if spec.Environment["MZ_PLUGIN_HOME"] == "" {
		t.Fatalf("声明环境变量应保留: %#v", spec.Environment)
	}
}

func TestManifestVerifiesArtifactSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	artifact := []byte("isolated plugin executable")
	manifestJSON := signedManifestJSON(t, privateKey, artifact)
	manifest, err := DecodeManifest([]byte(manifestJSON))
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.VerifyArtifactSignature(artifact, map[string]ed25519.PublicKey{"dev-ed25519": publicKey}); err != nil {
		t.Fatalf("签名真实且摘要匹配时应通过: %v", err)
	}
	if err := manifest.VerifyArtifactSignature([]byte("tampered"), map[string]ed25519.PublicKey{"dev-ed25519": publicKey}); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("制品摘要不匹配时应拒绝: %v", err)
	}
	if err := manifest.VerifyArtifactSignature(artifact, map[string]ed25519.PublicKey{"other-key": publicKey}); !errors.Is(err, ErrUnknownTrustRoot) {
		t.Fatalf("信任根缺失时应拒绝: %v", err)
	}
}

func TestManifestJSONSchemaIsValid(t *testing.T) {
	if !json.Valid(BuiltinSchemaJSON()) {
		t.Fatal("插件 manifest schema 必须是有效 JSON")
	}
}

func validManifestJSON() string {
	return manifestJSONWithSignature("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", signature())
}

func signature() string {
	return strings.Repeat("b", 128)
}

func signedManifestJSON(t *testing.T, privateKey ed25519.PrivateKey, artifact []byte) string {
	t.Helper()
	digest := ArtifactSHA256(artifact)
	payload := SigningPayload(SigningPayloadInput{
		PluginID:     "browser.bridge",
		ExecutableID: "mz-browser-bridge",
		SHA256:       digest,
	})
	return manifestJSONWithSignature(digest, hexSignature(ed25519.Sign(privateKey, payload)))
}

func manifestJSONWithSignature(sha256 string, signature string) string {
	return `{"$schema":"./manifest-v1.schema.json","schema_version":1,"plugin_id":"browser.bridge","display_name":"Browser Bridge","publisher":"mz-gui","update_channel":"stable","process":{"protocol_major":1,"protocol_minor":0,"executable_id":"mz-browser-bridge","build_id":"build-20260816","arguments_schema":"schema://browser.bridge/args-v1","health_check":"grpc.health.v1","shutdown_timeout_ms":5000},"requested_capabilities":["adapter.browser","transport.remote-service"],"resource_scopes":[{"kind":"origin","value":"https://example.com"}],"platforms":[{"os":"darwin","arch":"arm64"},{"os":"windows","arch":"amd64"}],"signature":{"key_id":"dev-ed25519","sha256":"` + sha256 + `","signature":"` + signature + `"},"isolation":{"workdir_mode":"private","env_allowlist":["MZ_PLUGIN_HOME"],"stdio":"framed-json","network":"declared-only"},"limits":{"max_memory_mb":256,"max_message_bytes":262144,"startup_timeout_ms":3000}}`
}

func hexSignature(signature []byte) string {
	const table = "0123456789abcdef"
	out := make([]byte, len(signature)*2)
	for i, value := range signature {
		out[i*2] = table[value>>4]
		out[i*2+1] = table[value&0x0f]
	}
	return string(out)
}
