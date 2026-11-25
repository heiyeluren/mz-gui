package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/updater"
)

func TestRunSignsManifestAndWritesTrustSpec(t *testing.T) {
	root := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "release.key")
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(privateKey)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(root, "payload.json")
	if err := os.WriteFile(payloadPath, []byte(validPayloadJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "latest.json")
	trustPath := filepath.Join(root, "trust.txt")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"--payload", payloadPath,
		"--private-key", keyPath,
		"--key-id", "release-2026",
		"--manifest", manifestPath,
		"--trust-spec", trustPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("签名失败: code=%d stderr=%s", code, stderr.String())
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := updater.ParseAndVerify(manifestData, updater.VerifyPolicy{
		Product: "mz-mozi-gateway", Channel: "stable",
		Keys: map[string]ed25519.PublicKey{"release-2026": publicKey},
	})
	if err != nil || manifest.Payload.Version != "1.2.3" {
		t.Fatalf("验签结果无效: manifest=%+v err=%v", manifest, err)
	}
	wantTrust := "release-2026=" + base64.StdEncoding.EncodeToString(publicKey) + "\n"
	trustData, err := os.ReadFile(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(trustData) != wantTrust || !strings.Contains(stdout.String(), "已生成签名清单") {
		t.Fatalf("发布输出错误: trust=%q stdout=%q", trustData, stdout.String())
	}
}

func TestRunRejectsInsecurePrivateKeyPermissions(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Windows 文件权限由 ACL 约束")
	}
	root := t.TempDir()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "release.key")
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(privateKey)), 0o644); err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(root, "payload.json")
	if err := os.WriteFile(payloadPath, []byte(validPayloadJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := run([]string{"--payload", payloadPath, "--private-key", keyPath, "--key-id", "release-2026", "--manifest", filepath.Join(root, "latest.json")}, &bytes.Buffer{}, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "权限") {
		t.Fatalf("宽松私钥权限未被拒绝: code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunRejectsSymlinkPrivateKey(t *testing.T) {
	root := t.TempDir()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	realKey := filepath.Join(root, "real.key")
	if err := os.WriteFile(realKey, []byte(base64.StdEncoding.EncodeToString(privateKey)), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedKey := filepath.Join(root, "linked.key")
	if err := os.Symlink(realKey, linkedKey); err != nil {
		t.Skipf("当前文件系统不支持符号链接: %v", err)
	}
	payloadPath := filepath.Join(root, "payload.json")
	if err := os.WriteFile(payloadPath, []byte(validPayloadJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := run([]string{"--payload", payloadPath, "--private-key", linkedKey, "--key-id", "release-2026", "--manifest", filepath.Join(root, "latest.json")}, &bytes.Buffer{}, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "符号链接") {
		t.Fatalf("私钥符号链接未被拒绝: code=%d stderr=%q", code, stderr.String())
	}
}

func validPayloadJSON() string {
	return `{
  "schema_version": 1,
  "product": "mz-mozi-gateway",
  "channel": "stable",
  "version": "1.2.3",
  "published_at": "2026-08-27T00:00:00Z",
  "expires_at": "2099-08-27T00:00:00Z",
  "artifacts": [
    {
      "os": "darwin",
      "arch": "arm64",
      "kind": "full",
      "url": "https://packages.example.com/gateway-darwin-arm64.zip",
      "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "size": 42
    }
  ]
}`
}
