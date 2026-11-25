package remoteservice

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
)

type signedTLSPinEnvelope struct {
	Payload   json.RawMessage `json:"payload"`
	KeyID     string          `json:"key_id"`
	Signature string          `json:"signature"`
}

func SignTLSPin(payload []byte, keyID string, private ed25519.PrivateKey) ([]byte, error) {
	if len(payload) == 0 || strings.TrimSpace(keyID) == "" || len(private) != ed25519.PrivateKeySize {
		return nil, ErrInvalidConfig
	}
	signature := ed25519.Sign(private, payload)
	return json.Marshal(signedTLSPinEnvelope{
		Payload: payload, KeyID: strings.TrimSpace(keyID), Signature: hex.EncodeToString(signature),
	})
}

func VerifySignedTLSPin(envelope []byte, keys map[string]ed25519.PublicKey) (TLSPinDocument, error) {
	var signed signedTLSPinEnvelope
	if err := json.Unmarshal(envelope, &signed); err != nil {
		return TLSPinDocument{}, ErrInvalidConfig
	}
	public, ok := keys[strings.TrimSpace(signed.KeyID)]
	if !ok || len(public) != ed25519.PublicKeySize {
		return TLSPinDocument{}, ErrInvalidConfig
	}
	rawSignature, err := hex.DecodeString(signed.Signature)
	if err != nil || !ed25519.Verify(public, signed.Payload, rawSignature) {
		return TLSPinDocument{}, ErrInvalidConfig
	}
	return ParseTLSPinDocument(signed.Payload)
}
