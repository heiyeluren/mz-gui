package remoteservice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestProxyCredentialVaultStoresPasswordOnlyBehindReference(t *testing.T) {
	store := newRecordingProxyCredentialStore()
	vault, err := NewProxyCredentialVault(ProxyCredentialVaultOptions{Store: store, Namespace: "proxy"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := vault.Save(context.Background(), ProxyCredentialRef{
		Host: "proxy.example.test", Port: 8080, Realm: "corp", Username: "alice",
	}, ProxyCredential{Username: "alice", Password: "p@ss-secret", Scheme: "Basic"})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Key != "proxy/proxy.example.test/8080/corp/alice" || ref.Host != "proxy.example.test" || ref.Port != 8080 {
		t.Fatalf("凭据引用异常: %#v", ref)
	}
	if strings.Contains(ref.String(), "p@ss-secret") {
		t.Fatalf("凭据引用泄漏密码: %s", ref.String())
	}
	raw := store.values[ref.Key]
	if !strings.Contains(raw, "p@ss-secret") || !strings.Contains(raw, "alice") {
		t.Fatalf("密码未写入凭据 store: %s", raw)
	}
	loaded, err := vault.Load(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Username != "alice" || loaded.Password != "p@ss-secret" || loaded.Scheme != "Basic" {
		t.Fatalf("读取代理凭据异常: %#v", loaded)
	}
	if strings.Join(store.calls, "|") != "set:proxy/proxy.example.test/8080/corp/alice|get:proxy/proxy.example.test/8080/corp/alice" {
		t.Fatalf("凭据 store 调用异常: %#v", store.calls)
	}
}

func TestProxyCredentialVaultDeletesAndRejectsInvalidRefs(t *testing.T) {
	store := newRecordingProxyCredentialStore()
	vault, err := NewProxyCredentialVault(ProxyCredentialVaultOptions{Store: store, Namespace: "proxy"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := vault.Save(context.Background(), ProxyCredentialRef{Host: "proxy.example.test", Port: 8080, Username: "alice"}, ProxyCredential{
		Username: "alice", Password: "secret", Scheme: "Basic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Delete(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Load(context.Background(), ref); !errors.Is(err, ErrProxyCredentialNotFound) {
		t.Fatalf("删除后应返回 not found: %v", err)
	}
	for _, candidate := range []ProxyCredentialRef{
		{Host: "", Port: 8080, Username: "alice"},
		{Host: "Proxy.Example.Test", Port: 8080, Username: "alice"},
		{Host: "proxy.example.test", Port: 0, Username: "alice"},
		{Host: "proxy.example.test", Port: 8080, Username: "Alice"},
		{Host: "user:pass@proxy.example.test", Port: 8080, Username: "alice"},
		{Host: "proxy.example.test", Port: 8080, Username: "alice", Key: "custom/bad"},
	} {
		if _, err := vault.Save(context.Background(), candidate, ProxyCredential{Username: "alice", Password: "secret"}); !errors.Is(err, ErrInvalidProxyCredentialRef) {
			t.Fatalf("非法 ref 必须拒绝: ref=%#v err=%v", candidate, err)
		}
	}
}

func TestProxyCredentialVaultRedactsStoreErrors(t *testing.T) {
	store := newRecordingProxyCredentialStore()
	store.setErr = errors.New("keychain failed with p@ss-secret in /Users/private/login.keychain")
	vault, err := NewProxyCredentialVault(ProxyCredentialVaultOptions{Store: store, Namespace: "proxy"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = vault.Save(context.Background(), ProxyCredentialRef{Host: "proxy.example.test", Port: 8080, Username: "alice"}, ProxyCredential{Username: "alice", Password: "p@ss-secret"})
	if !errors.Is(err, ErrProxyCredentialStoreUnavailable) {
		t.Fatalf("store 错误应脱敏映射: %v", err)
	}
	if strings.Contains(err.Error(), "p@ss-secret") || strings.Contains(err.Error(), "/Users/private") {
		t.Fatalf("错误泄漏敏感信息: %v", err)
	}
}

func TestProxyCredentialVaultBuildsProxyURLWithoutLeakingRef(t *testing.T) {
	store := newRecordingProxyCredentialStore()
	vault, err := NewProxyCredentialVault(ProxyCredentialVaultOptions{Store: store, Namespace: "proxy"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := vault.Save(context.Background(), ProxyCredentialRef{Host: "proxy.example.test", Port: 8080, Username: "alice"}, ProxyCredential{
		Username: "alice", Password: "p@ss-secret", Scheme: "Basic",
	})
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, err := vault.ProxyURL(context.Background(), "http://proxy.example.test:8080", ref)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL.User == nil {
		t.Fatal("代理 URL 缺少 userinfo")
	}
	password, ok := proxyURL.User.Password()
	if !ok || proxyURL.User.Username() != "alice" || password != "p@ss-secret" {
		t.Fatalf("代理 URL 凭据异常: %#v", proxyURL.User)
	}
	if strings.Contains(ref.String(), "p@ss-secret") {
		t.Fatalf("ref 泄漏密码: %s", ref.String())
	}
	if _, err := vault.ProxyURL(context.Background(), "http://other.example.test:8080", ref); !errors.Is(err, ErrInvalidProxyCredentialRef) {
		t.Fatalf("host 不一致必须拒绝: %v", err)
	}
}

type recordingProxyCredentialStore struct {
	mu        sync.Mutex
	values    map[string]string
	calls     []string
	setErr    error
	getErr    error
	deleteErr error
}

func newRecordingProxyCredentialStore() *recordingProxyCredentialStore {
	return &recordingProxyCredentialStore{values: make(map[string]string)}
}

func (store *recordingProxyCredentialStore) Get(_ context.Context, key string) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.calls = append(store.calls, "get:"+key)
	if store.getErr != nil {
		return "", store.getErr
	}
	value, ok := store.values[key]
	if !ok {
		return "", ErrProxyCredentialNotFound
	}
	return value, nil
}

func (store *recordingProxyCredentialStore) Set(_ context.Context, key, value string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.calls = append(store.calls, "set:"+key)
	if store.setErr != nil {
		return store.setErr
	}
	store.values[key] = value
	return nil
}

func (store *recordingProxyCredentialStore) Delete(_ context.Context, key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.calls = append(store.calls, "delete:"+key)
	if store.deleteErr != nil {
		return store.deleteErr
	}
	delete(store.values, key)
	return nil
}
