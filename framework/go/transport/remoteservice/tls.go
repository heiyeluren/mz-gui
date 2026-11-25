package remoteservice

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
)

type TLSPinSet struct {
	pins map[string]struct{}
}

func NewTLSPinSet(values []string) (TLSPinSet, error) {
	if len(values) == 0 {
		return TLSPinSet{}, ErrInvalidConfig
	}
	pins := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if !strings.HasPrefix(normalized, "sha256/") || len(normalized) <= len("sha256/") {
			return TLSPinSet{}, ErrInvalidConfig
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(normalized, "sha256/"))
		if err != nil || len(raw) != sha256.Size {
			return TLSPinSet{}, ErrInvalidConfig
		}
		pins[normalized] = struct{}{}
	}
	return TLSPinSet{pins: pins}, nil
}

func (pins TLSPinSet) Empty() bool {
	return len(pins.pins) == 0
}

func (pins TLSPinSet) VerifyPeerCertificates(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	if pins.Empty() {
		return nil
	}
	candidates := make([]*x509.Certificate, 0)
	for _, chain := range verifiedChains {
		if len(chain) > 0 && chain[0] != nil {
			candidates = append(candidates, chain[0])
		}
	}
	if len(candidates) == 0 && len(rawCerts) > 0 {
		leaf, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
		candidates = append(candidates, leaf)
	}
	for _, certificate := range candidates {
		pin, err := SPKISHA256Pin(certificate)
		if err != nil {
			return err
		}
		if _, ok := pins.pins[pin]; ok {
			return nil
		}
	}
	return ErrTLSPinMismatch
}

func SPKISHA256Pin(certificate *x509.Certificate) (string, error) {
	if certificate == nil || len(certificate.RawSubjectPublicKeyInfo) == 0 {
		return "", ErrInvalidRequest
	}
	sum := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	return "sha256/" + base64.StdEncoding.EncodeToString(sum[:]), nil
}
