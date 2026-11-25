package credentials

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeBackend struct {
	values    map[string]string
	getErr    error
	setErr    error
	deleteErr error
	calls     []string
}

func (f *fakeBackend) Get(_ string, key string) (string, error) {
	f.calls = append(f.calls, "get:"+key)
	if f.getErr != nil {
		return "", f.getErr
	}
	value, ok := f.values[key]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (f *fakeBackend) Set(_ string, key string, value string) error {
	f.calls = append(f.calls, "set:"+key)
	if f.setErr != nil {
		return f.setErr
	}
	if f.values == nil {
		f.values = make(map[string]string)
	}
	f.values[key] = value
	return nil
}

func (f *fakeBackend) Delete(_ string, key string) error {
	f.calls = append(f.calls, "delete:"+key)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.values[key]; !ok {
		return ErrNotFound
	}
	delete(f.values, key)
	return nil
}

func TestStoreRoundTrip(t *testing.T) {
	backend := &fakeBackend{values: make(map[string]string)}
	store, err := New(Options{Backend: backend, Service: "com.example.credentials"})
	if err != nil {
		t.Fatalf("创建凭据存储失败: %v", err)
	}

	ctx := context.Background()
	if err := store.Set(ctx, "feishu/default/app-id", "cli_fake_app_id"); err != nil {
		t.Fatalf("保存凭据失败: %v", err)
	}
	value, err := store.Get(ctx, "feishu/default/app-id")
	if err != nil {
		t.Fatalf("读取凭据失败: %v", err)
	}
	if value != "cli_fake_app_id" {
		t.Fatalf("读取值不一致: %q", value)
	}
	if err := store.Delete(ctx, "feishu/default/app-id"); err != nil {
		t.Fatalf("删除凭据失败: %v", err)
	}
	if _, err := store.Get(ctx, "feishu/default/app-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应返回 ErrNotFound，实际为 %v", err)
	}
}

func TestStoreRejectsInvalidLogicalKeysAndValues(t *testing.T) {
	store, err := New(Options{Backend: &fakeBackend{values: make(map[string]string)}, Service: "com.example.credentials"})
	if err != nil {
		t.Fatalf("创建凭据存储失败: %v", err)
	}

	invalidKeys := []string{"", "/feishu", "Feishu/default/app-id", "feishu//app-id", "../secret", "feishu/default/app id", strings.Repeat("a", 129)}
	for _, key := range invalidKeys {
		if err := store.Set(context.Background(), key, "value"); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("逻辑键 %q 应被拒绝，实际为 %v", key, err)
		}
	}

	invalidValues := []string{"", "contains\x00nul", "line\nbreak", strings.Repeat("a", 4097)}
	for _, value := range invalidValues {
		if err := store.Set(context.Background(), "feishu/default/app-id", value); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("非法值应被拒绝，实际为 %v", err)
		}
	}
}

func TestStoreMapsBackendErrorsWithoutLeakingDetails(t *testing.T) {
	secretError := errors.New("security command failed for cli_private_value in /Users/private/keychain")
	backend := &fakeBackend{values: make(map[string]string), getErr: secretError, setErr: secretError, deleteErr: secretError}
	store, err := New(Options{Backend: backend, Service: "com.example.credentials"})
	if err != nil {
		t.Fatalf("创建凭据存储失败: %v", err)
	}

	operations := []func() error{
		func() error { _, err := store.Get(context.Background(), "feishu/default/app-id"); return err },
		func() error { return store.Set(context.Background(), "feishu/default/app-id", "cli_fake_app_id") },
		func() error { return store.Delete(context.Background(), "feishu/default/app-id") },
	}
	for _, operation := range operations {
		err := operation()
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("底层错误应映射为 ErrUnavailable，实际为 %v", err)
		}
		if strings.Contains(err.Error(), "cli_private_value") || strings.Contains(err.Error(), "/Users/private") {
			t.Fatalf("错误泄漏了底层敏感信息: %v", err)
		}
	}
}

func TestStoreHonoursCancelledContextBeforeBackendCall(t *testing.T) {
	backend := &fakeBackend{values: make(map[string]string)}
	store, err := New(Options{Backend: backend, Service: "com.example.credentials"})
	if err != nil {
		t.Fatalf("创建凭据存储失败: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := store.Set(ctx, "feishu/default/app-id", "cli_fake_app_id"); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消上下文应原样返回，实际为 %v", err)
	}
	if len(backend.calls) != 0 {
		t.Fatalf("取消后不应访问系统凭据库，实际调用: %v", backend.calls)
	}
}

func TestNewRejectsMissingBackendAndInvalidService(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("缺少 backend 应返回 ErrInvalidArgument，实际为 %v", err)
	}
	if _, err := New(Options{Backend: &fakeBackend{}}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("缺少应用 service 应返回 ErrInvalidArgument，实际为 %v", err)
	}
	if _, err := New(Options{Backend: &fakeBackend{}, Service: "invalid service"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 service 应返回 ErrInvalidArgument，实际为 %v", err)
	}
}
