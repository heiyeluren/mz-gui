package remoteservice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOAuthTokenVaultStoresTokenOnlyBehindCredentialReference(t *testing.T) {
	store := newRecordingOAuthCredentialStore()
	vault, err := NewOAuthTokenVault(OAuthTokenVaultOptions{Store: store, Namespace: "oauth"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := vault.Save(context.Background(), OAuthTokenCredentialRef{
		Provider: "example", Account: "default",
	}, OAuthToken{
		AccessToken: "access-value", RefreshToken: "refresh-value", TokenType: "Bearer",
		Scope: "profile", ExpiresIn: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Key != "oauth/example/default" || ref.Provider != "example" || ref.Account != "default" {
		t.Fatalf("凭据引用异常: %#v", ref)
	}
	if strings.Contains(ref.String(), "access-value") || strings.Contains(ref.String(), "refresh-value") {
		t.Fatalf("凭据引用泄漏 token: %s", ref.String())
	}
	raw := store.values[ref.Key]
	if !strings.Contains(raw, "access-value") || !strings.Contains(raw, "refresh-value") {
		t.Fatalf("token 未写入凭据 store: %s", raw)
	}
	loaded, err := vault.Load(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccessToken != "access-value" || loaded.RefreshToken != "refresh-value" || loaded.ExpiresIn != time.Hour {
		t.Fatalf("读取 token 异常: %#v", loaded)
	}
	if strings.Join(store.calls, "|") != "set:oauth/example/default|get:oauth/example/default" {
		t.Fatalf("凭据 store 调用异常: %#v", store.calls)
	}
}

func TestOAuthTokenVaultDeletesAndRejectsInvalidRefs(t *testing.T) {
	store := newRecordingOAuthCredentialStore()
	vault, err := NewOAuthTokenVault(OAuthTokenVaultOptions{Store: store, Namespace: "oauth"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := vault.Save(context.Background(), OAuthTokenCredentialRef{Provider: "example", Account: "default"}, OAuthToken{
		AccessToken: "access-value", RefreshToken: "refresh-value", TokenType: "Bearer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Delete(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Load(context.Background(), ref); !errors.Is(err, ErrOAuthCredentialNotFound) {
		t.Fatalf("删除后应返回 not found: %v", err)
	}
	for _, candidate := range []OAuthTokenCredentialRef{
		{Provider: "", Account: "default"},
		{Provider: "Example", Account: "default"},
		{Provider: "example", Account: "../default"},
		{Provider: "example", Account: "default", Key: "custom/bad"},
	} {
		if _, err := vault.Save(context.Background(), candidate, OAuthToken{AccessToken: "a"}); !errors.Is(err, ErrInvalidOAuthCredentialRef) {
			t.Fatalf("非法 ref 必须拒绝: ref=%#v err=%v", candidate, err)
		}
	}
}

func TestOAuthTokenVaultRedactsStoreErrors(t *testing.T) {
	store := newRecordingOAuthCredentialStore()
	store.setErr = errors.New("keychain failed with access-value in /Users/private/login.keychain")
	vault, err := NewOAuthTokenVault(OAuthTokenVaultOptions{Store: store, Namespace: "oauth"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = vault.Save(context.Background(), OAuthTokenCredentialRef{Provider: "example", Account: "default"}, OAuthToken{AccessToken: "access-value"})
	if !errors.Is(err, ErrOAuthCredentialStoreUnavailable) {
		t.Fatalf("store 错误应脱敏映射: %v", err)
	}
	if strings.Contains(err.Error(), "access-value") || strings.Contains(err.Error(), "/Users/private") {
		t.Fatalf("错误泄漏敏感信息: %v", err)
	}
}

type recordingOAuthCredentialStore struct {
	mu        sync.Mutex
	values    map[string]string
	calls     []string
	getErr    error
	setErr    error
	deleteErr error
}

func newRecordingOAuthCredentialStore() *recordingOAuthCredentialStore {
	return &recordingOAuthCredentialStore{values: make(map[string]string)}
}

func (store *recordingOAuthCredentialStore) Get(_ context.Context, key string) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.calls = append(store.calls, "get:"+key)
	if store.getErr != nil {
		return "", store.getErr
	}
	value, ok := store.values[key]
	if !ok {
		return "", ErrOAuthCredentialNotFound
	}
	return value, nil
}

func (store *recordingOAuthCredentialStore) Set(_ context.Context, key string, value string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.calls = append(store.calls, "set:"+key)
	if store.setErr != nil {
		return store.setErr
	}
	store.values[key] = value
	return nil
}

func (store *recordingOAuthCredentialStore) Delete(_ context.Context, key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.calls = append(store.calls, "delete:"+key)
	if store.deleteErr != nil {
		return store.deleteErr
	}
	if _, ok := store.values[key]; !ok {
		return ErrOAuthCredentialNotFound
	}
	delete(store.values, key)
	return nil
}
