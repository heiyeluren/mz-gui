package remoteservice

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestVerifySignedTLSPinAcceptsKnownKey(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(TLSPinDocument{
		Version: "2026-08-16", NotBefore: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		NotAfter: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), CurrentPins: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	})
	envelope, err := SignTLSPin(payload, "pin-key-1", private)
	if err != nil {
		t.Fatal(err)
	}
	document, err := VerifySignedTLSPin(envelope, map[string]ed25519.PublicKey{"pin-key-1": public})
	if err != nil || document.Version != "2026-08-16" {
		t.Fatalf("验签失败: %#v %v", document, err)
	}
}

func TestVerifySignedTLSPinRejectsUnknownKeyAndTamper(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(TLSPinDocument{
		Version: "v1", NotBefore: time.Now().Add(-time.Hour).UTC(), NotAfter: time.Now().Add(time.Hour).UTC(),
		CurrentPins: []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	})
	envelope, err := SignTLSPin(payload, "pin-key-1", private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySignedTLSPin(envelope, map[string]ed25519.PublicKey{"other": public}); err == nil {
		t.Fatal("未知 key 必须拒绝")
	}
	var parsed map[string]any
	_ = json.Unmarshal(envelope, &parsed)
	parsed["signature"] = hex.EncodeToString(make([]byte, ed25519.SignatureSize))
	tampered, _ := json.Marshal(parsed)
	if _, err := VerifySignedTLSPin(tampered, map[string]ed25519.PublicKey{"pin-key-1": public}); err == nil {
		t.Fatal("篡改签名必须拒绝")
	}
}
