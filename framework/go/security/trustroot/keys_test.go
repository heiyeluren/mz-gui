package trustroot

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestParseAcceptsMultiplePublicKeys(t *testing.T) {
	first, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	spec := "release-2026=" + base64.StdEncoding.EncodeToString(first) + ";backup.1=" + base64.StdEncoding.EncodeToString(second)
	keys, err := Parse(spec)
	if err != nil {
		t.Fatalf("解析信任根: %v", err)
	}
	if len(keys) != 2 || !first.Equal(keys["release-2026"]) || !second.Equal(keys["backup.1"]) {
		t.Fatalf("信任根错误: %#v", keys)
	}
	keys["release-2026"][0] ^= 0xff
	if first.Equal(keys["release-2026"]) {
		t.Fatal("解析结果没有复制公钥")
	}
}

func TestParseFailsClosed(t *testing.T) {
	invalid := []string{
		"bad",
		"../key=YWJj",
		"key=YWJj",
		"key=YWJj;key=YWJj",
		" key=YWJj",
	}
	for _, value := range invalid {
		if _, err := Parse(value); err == nil {
			t.Fatalf("非法信任根未拒绝: %q", value)
		}
	}
	keys, err := Parse("")
	if err != nil || len(keys) != 0 {
		t.Fatalf("空信任根应安全禁用: keys=%v err=%v", keys, err)
	}
}
