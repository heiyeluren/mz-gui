package leakscan

import "testing"

func TestScanFindsSecretsAndIgnoresSafeText(t *testing.T) {
	findings := Scan("token=abc123 password=hunter2 -----BEGIN PRIVATE KEY----- ok")
	if len(findings) < 3 {
		t.Fatalf("应检出 secret: %#v", findings)
	}
	if findings := Scan("status=ok revision=4"); len(findings) != 0 {
		t.Fatalf("安全文本不得误报: %#v", findings)
	}
}

func TestScanRejectsEmptyInputAsNoFindings(t *testing.T) {
	if findings := Scan(""); len(findings) != 0 {
		t.Fatalf("%#v", findings)
	}
}
