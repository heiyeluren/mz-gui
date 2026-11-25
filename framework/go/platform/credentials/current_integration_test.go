package credentials

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestCurrentStoreRoundTrip(t *testing.T) {
	if os.Getenv("MZ_GUI_REAL_KEYRING_TEST") != "1" {
		t.Skip("仅在显式启用时访问当前用户系统凭据库")
	}
	store, err := NewCurrent("com.example.mzgui.credentials-test")
	if err != nil {
		t.Fatalf("创建系统凭据存储失败: %v", err)
	}
	ctx := context.Background()
	key := "test/integration/roundtrip"
	_ = store.Delete(ctx, key)
	t.Cleanup(func() {
		_ = store.Delete(context.Background(), key)
	})

	if err := store.Set(ctx, key, "mz-gui-keyring-test-value"); err != nil {
		t.Fatalf("写入系统凭据库失败: %v", err)
	}
	value, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("读取系统凭据库失败: %v", err)
	}
	if value != "mz-gui-keyring-test-value" {
		t.Fatal("系统凭据库回读值不一致")
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("删除系统凭据失败: %v", err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应返回 ErrNotFound，实际为 %v", err)
	}
}
