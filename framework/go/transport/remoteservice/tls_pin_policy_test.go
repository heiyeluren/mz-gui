package remoteservice

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"
)

func TestTLSPinPolicyAcceptsCurrentAndNextDuringOverlap(t *testing.T) {
	currentCert := mustLeafCertificate(t)
	nextCert := mustLeafCertificate(t)
	currentPin, err := SPKISHA256Pin(currentCert)
	if err != nil {
		t.Fatal(err)
	}
	nextPin, err := SPKISHA256Pin(nextCert)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)
	policy, err := NewTLSPinPolicy(TLSPinDocument{
		Version:      "1",
		NotBefore:    now.Add(-time.Hour),
		NextActiveAt: now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		CurrentPins:  []string{currentPin},
		NextPins:     []string{nextPin},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.VerifyPeerCertificates(now, [][]byte{currentCert.Raw}, nil); err != nil {
		t.Fatalf("重叠窗口应接受 current pin: %v", err)
	}
	if err := policy.VerifyPeerCertificates(now, [][]byte{nextCert.Raw}, nil); err != nil {
		t.Fatalf("重叠窗口应接受 next pin: %v", err)
	}
}

func TestTLSPinPolicyRejectsNextPinBeforeOverlap(t *testing.T) {
	currentCert := mustLeafCertificate(t)
	nextCert := mustLeafCertificate(t)
	currentPin, err := SPKISHA256Pin(currentCert)
	if err != nil {
		t.Fatal(err)
	}
	nextPin, err := SPKISHA256Pin(nextCert)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)
	policy, err := NewTLSPinPolicy(TLSPinDocument{
		Version:      "1",
		NotBefore:    now.Add(-time.Hour),
		NextActiveAt: now.Add(time.Hour),
		NotAfter:     now.Add(2 * time.Hour),
		CurrentPins:  []string{currentPin},
		NextPins:     []string{nextPin},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.VerifyPeerCertificates(now, [][]byte{currentCert.Raw}, nil); err != nil {
		t.Fatalf("轮换前应接受 current pin: %v", err)
	}
	if err := policy.VerifyPeerCertificates(now, [][]byte{nextCert.Raw}, nil); !errors.Is(err, ErrTLSPinMismatch) {
		t.Fatalf("轮换前不得接受 next pin: %v", err)
	}
}

func TestTLSPinPolicyRotatePromotesNextAndKeepsPrevious(t *testing.T) {
	currentCert := mustLeafCertificate(t)
	nextCert := mustLeafCertificate(t)
	currentPin, err := SPKISHA256Pin(currentCert)
	if err != nil {
		t.Fatal(err)
	}
	nextPin, err := SPKISHA256Pin(nextCert)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)
	policy, err := NewTLSPinPolicy(TLSPinDocument{
		Version:      "1",
		NotBefore:    now.Add(-time.Hour),
		NextActiveAt: now,
		NotAfter:     now.Add(time.Hour),
		CurrentPins:  []string{currentPin},
		NextPins:     []string{nextPin},
	})
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := policy.Rotate(now)
	if err != nil {
		t.Fatal(err)
	}
	if err := rotated.VerifyPeerCertificates(now, [][]byte{nextCert.Raw}, nil); err != nil {
		t.Fatalf("轮换后 next 应成为 current: %v", err)
	}
	if err := rotated.VerifyPeerCertificates(now, [][]byte{currentCert.Raw}, nil); err != nil {
		t.Fatalf("轮换后旧 current 应作为 previous 保留: %v", err)
	}
	if len(rotated.Document().NextPins) != 0 || len(rotated.Document().PreviousPins) != 1 {
		t.Fatalf("轮换后文档状态异常: %#v", rotated.Document())
	}
}

func TestParseTLSPinDocumentRejectsExpiredAndEmptyCurrent(t *testing.T) {
	now := time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)
	validPin := "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	raw, err := json.Marshal(TLSPinDocument{
		Version:     "1",
		NotBefore:   now.Add(-2 * time.Hour),
		NotAfter:    now.Add(-time.Hour),
		CurrentPins: []string{validPin},
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseTLSPinDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewTLSPinPolicy(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.PinSetAt(now); !errors.Is(err, ErrTLSPinPolicyExpired) {
		t.Fatalf("过期文档必须拒绝: %v", err)
	}
	if _, err := NewTLSPinPolicy(TLSPinDocument{Version: "1", NotBefore: now, NotAfter: now.Add(time.Hour)}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("空 current pins 必须拒绝: %v", err)
	}
}

func mustLeafCertificate(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "pin-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}
