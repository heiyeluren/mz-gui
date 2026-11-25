package appauth

import (
	"errors"
	"testing"
)

func TestValidateVerificationRequiresNativeTrustAndExactIdentity(t *testing.T) {
	verified := Verification{Valid: true, Identifier: "com.example.demo", SignerIdentity: "TEAM123", Source: "codesign"}
	if err := ValidateVerification(verified, "com.example.demo", "TEAM123"); err != nil {
		t.Fatalf("可信原生签名不应被拒绝: %v", err)
	}
	for name, candidate := range map[string]Verification{
		"未通过原生校验": {Identifier: "com.example.demo", SignerIdentity: "TEAM123"},
		"应用标识不符":  {Valid: true, Identifier: "com.example.other", SignerIdentity: "TEAM123", Source: "codesign"},
		"签名身份不符":  {Valid: true, Identifier: "com.example.demo", SignerIdentity: "TEAM999", Source: "codesign"},
		"校验来源缺失":  {Valid: true, Identifier: "com.example.demo", SignerIdentity: "TEAM123"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateVerification(candidate, "com.example.demo", "TEAM123"); !errors.Is(err, ErrUntrustedApplication) {
				t.Fatalf("应拒绝不可信原生签名: %v", err)
			}
		})
	}
}
