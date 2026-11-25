package artifact

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAndVerifyReleaseCatalog(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := validReleasePayload()
	data, err := SignReleaseCatalog(payload, "asset-release-2026", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ParseAndVerifyReleaseCatalog(data, ReleasePolicy{
		Namespace: "mozi.asset-hub", Channel: "stable",
		Keys: map[string]ed25519.PublicKey{"asset-release-2026": publicKey},
		Now:  func() time.Time { return time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("验签发布目录: %v", err)
	}
	release, err := catalog.Resolve("skills", "123e4567-e89b-12d3-a456-426614174000", "v1.2.3", "darwin", "arm64")
	if err != nil || release.SHA256 != strings.Repeat("a", 64) || catalog.KeyID != "asset-release-2026" {
		t.Fatalf("解析发布制品错误: release=%#v catalog=%#v err=%v", release, catalog, err)
	}
}

func TestReleaseCatalogRejectsTamperingAndPolicyMismatch(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := SignReleaseCatalog(validReleasePayload(), "trusted", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	policy := ReleasePolicy{Namespace: "mozi.asset-hub", Channel: "stable", Keys: map[string]ed25519.PublicKey{"trusted": publicKey},
		Now: func() time.Time { return time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC) }}

	tests := []struct {
		name   string
		data   []byte
		policy ReleasePolicy
		target error
	}{
		{name: "未知密钥", data: valid, policy: ReleasePolicy{Namespace: policy.Namespace, Channel: policy.Channel, Keys: map[string]ed25519.PublicKey{}, Now: policy.Now}, target: ErrReleaseUntrusted},
		{name: "产品空间不匹配", data: valid, policy: ReleasePolicy{Namespace: "other.product", Channel: policy.Channel, Keys: policy.Keys, Now: policy.Now}, target: ErrReleasePolicy},
		{name: "目录过期", data: valid, policy: ReleasePolicy{Namespace: policy.Namespace, Channel: policy.Channel, Keys: policy.Keys,
			Now: func() time.Time { return time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC) }}, target: ErrReleaseExpired},
		{name: "尾随内容", data: append(append([]byte(nil), valid...), []byte(" trailing")...), policy: policy, target: ErrReleaseManifest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseAndVerifyReleaseCatalog(test.data, test.policy); !errors.Is(err, test.target) {
				t.Fatalf("错误 = %v, 期望 %v", err, test.target)
			}
		})
	}

	var envelope ReleaseEnvelope
	if err := json.Unmarshal(valid, &envelope); err != nil {
		t.Fatal(err)
	}
	invalidKey := envelope
	invalidKey.KeyID = "../trusted"
	invalidKeyData, _ := json.Marshal(invalidKey)
	if _, err := ParseAndVerifyReleaseCatalog(invalidKeyData, policy); !errors.Is(err, ErrReleaseManifest) {
		t.Fatalf("非法 key ID 错误 = %v", err)
	}
	envelope.Payload.Artifacts[0].SHA256 = strings.Repeat("b", 64)
	tampered, _ := json.Marshal(envelope)
	if _, err := ParseAndVerifyReleaseCatalog(tampered, policy); !errors.Is(err, ErrReleaseSignature) {
		t.Fatalf("篡改签名错误 = %v", err)
	}
}

func TestReleaseCatalogResolveEnforcesIdentityAndPlatform(t *testing.T) {
	catalog := ReleaseCatalog{Payload: validReleasePayload()}
	tests := []struct {
		kind, id, version, goos, goarch string
		target                          error
	}{
		{kind: "skills", id: "123e4567-e89b-12d3-a456-426614174000", version: "v1.2.4", goos: "darwin", goarch: "arm64", target: ErrReleaseNotFound},
		{kind: "mac-apps", id: "123e4567-e89b-12d3-a456-426614174000", version: "v1.2.3", goos: "windows", goarch: "amd64", target: ErrReleaseNotFound},
		{kind: "../skills", id: "123e4567-e89b-12d3-a456-426614174000", version: "v1.2.3", goos: "darwin", goarch: "arm64", target: ErrReleaseRequest},
	}
	for _, test := range tests {
		if _, err := catalog.Resolve(test.kind, test.id, test.version, test.goos, test.goarch); !errors.Is(err, test.target) {
			t.Fatalf("Resolve(%q) 错误 = %v, 期望 %v", test.kind, err, test.target)
		}
	}
}

func TestVerifyReleaseArtifactBindsDigestSizeFormatAndArchive(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "package.zip")
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, _ := writer.Create("skill/SKILL.md")
	_, _ = entry.Write([]byte("# Demo"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	digest, bytes, err := fileSHA256(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	release := ReleaseArtifact{ID: "123e4567-e89b-12d3-a456-426614174000", Kind: "skills", Version: "v1.2.3",
		SHA256: digest, Bytes: bytes, Format: "zip", OS: "any", Arch: "any", ContractVersion: 1}
	verified, err := VerifyReleaseArtifact(archivePath, release, Policy{MaxEntries: 20, MaxExtractedBytes: 1 << 20, MaxPathBytes: 240})
	if err != nil || verified.SHA256 != digest || verified.Inspection.Entries != 1 {
		t.Fatalf("复验制品失败: verified=%#v err=%v", verified, err)
	}
	release.SHA256 = strings.Repeat("f", 64)
	if _, err := VerifyReleaseArtifact(archivePath, release, Policy{}); !errors.Is(err, ErrReleaseArtifactMismatch) {
		t.Fatalf("摘要不匹配错误 = %v", err)
	}
	release.SHA256 = digest
	release.Bytes++
	if _, err := VerifyReleaseArtifact(archivePath, release, Policy{}); !errors.Is(err, ErrReleaseArtifactMismatch) {
		t.Fatalf("大小不匹配错误 = %v", err)
	}
}

func validReleasePayload() ReleasePayload {
	return ReleasePayload{
		SchemaVersion: 1, Namespace: "mozi.asset-hub", Channel: "stable",
		GeneratedAt: "2026-08-15T08:00:00Z", ExpiresAt: "2026-08-16T08:00:00Z",
		Artifacts: []ReleaseArtifact{{
			ID: "123e4567-e89b-12d3-a456-426614174000", Kind: "skills", Version: "v1.2.3",
			SHA256: strings.Repeat("a", 64), Bytes: 1024, Format: "tar.gz", OS: "any", Arch: "any", ContractVersion: 1,
		}},
	}
}
