package remoteservice

import (
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTLSPinSetVerifiesSPKIPinAndSupportsRotation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	leaf := server.Certificate()
	pin, err := SPKISHA256Pin(leaf)
	if err != nil {
		t.Fatal(err)
	}
	backupPin := "sha256/0000000000000000000000000000000000000000000="
	pins, err := NewTLSPinSet([]string{backupPin, pin})
	if err != nil {
		t.Fatal(err)
	}
	if err := pins.VerifyPeerCertificates([][]byte{leaf.Raw}, nil); err != nil {
		t.Fatalf("正确 pin 应通过: %v", err)
	}

	wrongPins, err := NewTLSPinSet([]string{backupPin})
	if err != nil {
		t.Fatal(err)
	}
	if err := wrongPins.VerifyPeerCertificates([][]byte{leaf.Raw}, nil); !errors.Is(err, ErrTLSPinMismatch) {
		t.Fatalf("错误 pin 应拒绝，实际: %v", err)
	}
}

func TestRemoteClientUsesTLSPinsAfterTrustValidation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()

	rootPool := x509.NewCertPool()
	rootPool.AddCert(server.Certificate())
	pin, err := SPKISHA256Pin(server.Certificate())
	if err != nil {
		t.Fatal(err)
	}
	pins, err := NewTLSPinSet([]string{pin})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(ClientOptions{
		Policy:  policyForServer(t, server.URL),
		TLSRoot: rootPool,
		TLSPins: pins,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(response.Body) != "ok" {
		t.Fatalf("响应错误: %#v body=%q", response, response.Body)
	}

	wrongPins, err := NewTLSPinSet([]string{"sha256/0000000000000000000000000000000000000000000="})
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := NewClient(ClientOptions{
		Policy:  policyForServer(t, server.URL),
		TLSRoot: rootPool,
		TLSPins: wrongPins,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blocked.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL}); !errors.Is(err, ErrTLSPinMismatch) {
		t.Fatalf("错误 pin 连接应失败，实际: %v", err)
	}
}
