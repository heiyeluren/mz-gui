package updater

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseAndVerifyManifestSelectsPlatformArtifact(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := validPayload()
	data := signedManifest(t, "release-2026", privateKey, payload)

	manifest, err := ParseAndVerify(data, VerifyPolicy{
		Product: "mz-gui-desktop", Channel: "stable",
		Keys: map[string]ed25519.PublicKey{"release-2026": publicKey},
		Now:  func() time.Time { return time.Date(2026, 8, 15, 8, 30, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("验签更新清单: %v", err)
	}
	artifact, err := manifest.SelectArtifact("windows", "arm64")
	if err != nil {
		t.Fatalf("选择 Windows ARM64 制品: %v", err)
	}
	if artifact.Kind != "installer" || !strings.HasSuffix(artifact.URL, "windows-arm64.exe") {
		t.Fatalf("选择了错误制品: %#v", artifact)
	}
	if manifest.KeyID != "release-2026" || manifest.Payload.Version != "1.4.0" {
		t.Fatalf("清单字段错误: %#v", manifest)
	}
}

func TestSignProducesVerifiableManifestAndRejectsInvalidInput(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Sign(validPayload(), "release-2026", privateKey)
	if err != nil {
		t.Fatalf("签名更新清单: %v", err)
	}
	if _, err := ParseAndVerify(data, VerifyPolicy{
		Product: "mz-gui-desktop", Channel: "stable",
		Keys: map[string]ed25519.PublicKey{"release-2026": publicKey},
		Now:  func() time.Time { return time.Date(2026, 8, 15, 8, 30, 0, 0, time.UTC) },
	}); err != nil {
		t.Fatalf("验签由 Sign 生成的清单: %v", err)
	}
	if _, err := Sign(validPayload(), "../release", privateKey); !errors.Is(err, ErrInvalidSigningKey) {
		t.Fatalf("非法密钥标识错误 = %v", err)
	}
	if _, err := Sign(validPayload(), "release-2026", ed25519.PrivateKey("short")); !errors.Is(err, ErrInvalidSigningKey) {
		t.Fatalf("非法私钥错误 = %v", err)
	}
	invalidPayload := validPayload()
	invalidPayload.Artifacts[0].SHA256 = "bad"
	if _, err := Sign(invalidPayload, "release-2026", privateKey); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("非法发布内容错误 = %v", err)
	}
}

func TestParseAndVerifyManifestRejectsUntrustedOrTamperedInput(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := VerifyPolicy{
		Product: "mz-gui-desktop", Channel: "stable",
		Keys: map[string]ed25519.PublicKey{"release-2026": publicKey},
		Now:  func() time.Time { return time.Date(2026, 8, 15, 8, 30, 0, 0, time.UTC) },
	}

	tests := []struct {
		name   string
		mutate func(Envelope) Envelope
		target error
	}{
		{name: "未知密钥", mutate: func(value Envelope) Envelope { value.KeyID = "unknown"; return value }, target: ErrUntrustedManifest},
		{name: "签名被篡改", mutate: func(value Envelope) Envelope {
			value.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
			return value
		}, target: ErrInvalidSignature},
		{name: "产品不匹配", mutate: func(value Envelope) Envelope {
			value.Payload.Product = "other-desktop"
			return resign(t, value, privateKey)
		}, target: ErrManifestPolicy},
		{name: "渠道不匹配", mutate: func(value Envelope) Envelope { value.Payload.Channel = "beta"; return resign(t, value, privateKey) }, target: ErrManifestPolicy},
		{name: "清单过期", mutate: func(value Envelope) Envelope {
			value.Payload.ExpiresAt = "2026-08-15T08:15:00Z"
			return resign(t, value, privateKey)
		}, target: ErrManifestExpired},
		{name: "摘要非法", mutate: func(value Envelope) Envelope {
			value.Payload.Artifacts[0].SHA256 = "bad"
			return resign(t, value, privateKey)
		}, target: ErrInvalidManifest},
		{name: "非 HTTPS", mutate: func(value Envelope) Envelope {
			value.Payload.Artifacts[0].URL = "http://download.example.test/app.dmg"
			return resign(t, value, privateKey)
		}, target: ErrInvalidManifest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var envelope Envelope
			if err := json.Unmarshal(signedManifest(t, "release-2026", privateKey, validPayload()), &envelope); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(test.mutate(envelope))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseAndVerify(data, policy); !errors.Is(err, test.target) {
				t.Fatalf("错误 = %v, 期望 %v", err, test.target)
			}
		})
	}
}

func TestParseAndVerifyManifestRejectsUnknownFieldsAndDuplicateArtifacts(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := VerifyPolicy{Product: "mz-gui-desktop", Channel: "stable", Keys: map[string]ed25519.PublicKey{"key": publicKey}}
	data := signedManifest(t, "key", privateKey, validPayload())
	data = []byte(strings.Replace(string(data), `"key_id":"key"`, `"key_id":"key","unexpected":true`, 1))
	if _, err := ParseAndVerify(data, policy); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("未知字段错误 = %v", err)
	}
	if _, err := ParseAndVerify(append(signedManifest(t, "key", privateKey, validPayload()), []byte(" trailing")...), policy); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("尾随内容错误 = %v", err)
	}

	payload := validPayload()
	payload.Artifacts = append(payload.Artifacts, payload.Artifacts[0])
	if _, err := ParseAndVerify(signedManifest(t, "key", privateKey, payload), policy); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("重复制品错误 = %v", err)
	}
}

func TestEvaluateVersionPolicy(t *testing.T) {
	manifest := Manifest{Payload: validPayload()}
	tests := []struct {
		name      string
		current   string
		available bool
		forced    bool
	}{
		{name: "需要更新", current: "1.3.9", available: true},
		{name: "低于最低版本", current: "1.1.9", available: true, forced: true},
		{name: "已经最新", current: "1.4.0"},
		{name: "本地更高", current: "1.5.0"},
		{name: "预发布低于正式版", current: "1.4.0-beta.2", available: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := manifest.Evaluate(test.current)
			if err != nil {
				t.Fatal(err)
			}
			if decision.UpdateAvailable != test.available || decision.ForceRequired != test.forced {
				t.Fatalf("版本决策错误: %#v", decision)
			}
		})
	}

	manifest.Payload.ForceUpdate = true
	decision, err := manifest.Evaluate("1.3.0")
	if err != nil || !decision.ForceRequired {
		t.Fatalf("强制更新决策错误: %#v err=%v", decision, err)
	}
	if _, err := manifest.Evaluate("1.4"); !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("非法当前版本错误 = %v", err)
	}
}

func TestCompareVersionsFollowsSemanticVersionPrecedence(t *testing.T) {
	ordered := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta",
		"1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0",
	}
	for index := 0; index < len(ordered)-1; index++ {
		comparison, err := CompareVersions(ordered[index], ordered[index+1])
		if err != nil || comparison >= 0 {
			t.Fatalf("版本顺序错误: %s < %s, comparison=%d err=%v", ordered[index], ordered[index+1], comparison, err)
		}
	}
	comparison, err := CompareVersions("v1.2.3+build.1", "1.2.3+build.9")
	if err != nil || comparison != 0 {
		t.Fatalf("构建元数据不应改变优先级: comparison=%d err=%v", comparison, err)
	}
}

func TestPlanUpdatePrefersMatchingDeltaAndRequiresBaseHash(t *testing.T) {
	base := strings.Repeat("a", 64)
	target := strings.Repeat("c", 64)
	manifest := Manifest{Payload: Payload{Artifacts: []Artifact{
		{OS: "darwin", Arch: "arm64", Kind: "delta", SHA256: target, BaseSHA256: base},
		{OS: "darwin", Arch: "arm64", Kind: "full", SHA256: strings.Repeat("d", 64)},
	}}}
	plan, err := manifest.PlanUpdate("darwin", "arm64", base)
	if err != nil || plan.Preferred.Kind != "delta" || plan.Fallback.Kind != "full" {
		t.Fatalf("匹配基础摘要时应选择差分: %#v err=%v", plan, err)
	}
	plan, err = manifest.PlanUpdate("darwin", "arm64", strings.Repeat("b", 64))
	if err != nil || plan.Preferred.Kind != "full" || plan.Fallback.SHA256 != "" {
		t.Fatalf("基础不匹配时应选择完整包: %#v err=%v", plan, err)
	}

	payload := validPayload()
	payload.Artifacts = []Artifact{{
		OS: "darwin", Arch: "arm64", Kind: "delta", URL: "https://download.example.test/delta.zip",
		SHA256: strings.Repeat("e", 64), Size: 32,
	}}
	if err := validatePayload(payload); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("缺少基础摘要的差分制品应拒绝: %v", err)
	}
}

func TestManifestSelectsGenericFullPackage(t *testing.T) {
	manifest := Manifest{Payload: Payload{Artifacts: []Artifact{
		{OS: "windows", Arch: "amd64", Kind: "full", URL: "https://packages.example.com/runtime.zip"},
		{OS: "darwin", Arch: "universal", Kind: "full", URL: "https://packages.example.com/runtime.zip"},
	}}}
	windows, err := manifest.SelectArtifactKind("windows", "amd64", "full")
	if err != nil || windows.Arch != "amd64" {
		t.Fatalf("选择 Windows 通用制品失败: artifact=%#v err=%v", windows, err)
	}
	darwin, err := manifest.SelectArtifactKind("darwin", "arm64", "full")
	if err != nil || darwin.Arch != "universal" {
		t.Fatalf("选择 macOS Universal 制品失败: artifact=%#v err=%v", darwin, err)
	}
	if _, err := manifest.SelectArtifactKind("windows", "arm64", "full"); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("缺少架构时错误=%v", err)
	}
}

func validPayload() Payload {
	return Payload{
		SchemaVersion: 1, Product: "mz-gui-desktop", Channel: "stable", Version: "1.4.0",
		MinimumSupportedVersion: "1.2.0", ReleaseNotes: "安全与稳定性更新",
		PublishedAt: "2026-08-15T08:00:00Z", ExpiresAt: "2026-08-16T08:00:00Z",
		Artifacts: []Artifact{
			{OS: "darwin", Arch: "arm64", Kind: "installer", URL: "https://download.example.test/releases/macos-arm64.dmg", SHA256: strings.Repeat("a", 64), Size: 1024},
			{OS: "windows", Arch: "arm64", Kind: "installer", URL: "https://download.example.test/releases/windows-arm64.exe", SHA256: strings.Repeat("b", 64), Size: 2048},
		},
	}
}

func signedManifest(t *testing.T, keyID string, privateKey ed25519.PrivateKey, payload Payload) []byte {
	t.Helper()
	payloadData, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	envelope := Envelope{KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payloadData)), Payload: payload}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func resign(t *testing.T, value Envelope, privateKey ed25519.PrivateKey) Envelope {
	t.Helper()
	payload, err := json.Marshal(value.Payload)
	if err != nil {
		t.Fatal(err)
	}
	value.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return value
}
