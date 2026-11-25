package authorization

import (
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestOneTimeGrantBindsSubjectActionAndResource(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store, err := NewOneTimeGrantStore(OneTimeGrantOptions{
		TTL: 30 * time.Second, Capacity: 4, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	token, expiresAt, err := store.Issue("desktop-session", "claude.takeover", "revision:5")
	if err != nil || token == "" || !expiresAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("签发结果无效: token=%q expires=%v err=%v", token, expiresAt, err)
	}
	store.mu.Lock()
	for fingerprint := range store.entries {
		if hex.EncodeToString(fingerprint[:]) == token {
			store.mu.Unlock()
			t.Fatal("授权存储不得保留明文 token")
		}
	}
	store.mu.Unlock()
	if err := store.Consume(token, "other-session", "claude.takeover", "revision:5"); !errors.Is(err, ErrInvalidOneTimeGrant) {
		t.Fatalf("错误身份未拒绝: %v", err)
	}
	if err := store.Consume(token, "desktop-session", "claude.takeover", "revision:5"); !errors.Is(err, ErrInvalidOneTimeGrant) {
		t.Fatalf("绑定失败后 token 应立即失效: %v", err)
	}

	token, _, err = store.Issue("desktop-session", "claude.restore", "revision:5")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(token, "desktop-session", "claude.takeover", "revision:5"); !errors.Is(err, ErrInvalidOneTimeGrant) {
		t.Fatalf("错误动作未拒绝: %v", err)
	}

	token, _, err = store.Issue("desktop-session", "claude.restore", "revision:5")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(token, "desktop-session", "claude.restore", "revision:6"); !errors.Is(err, ErrInvalidOneTimeGrant) {
		t.Fatalf("错误资源未拒绝: %v", err)
	}
}

func TestOneTimeGrantExpiresAndCanOnlyBeConsumedOnce(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store, err := NewOneTimeGrantStore(OneTimeGrantOptions{
		TTL: time.Second, Capacity: 2, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.Issue("desktop-session", "claude.takeover", "revision:5")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(token, "desktop-session", "claude.takeover", "revision:5"); err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(token, "desktop-session", "claude.takeover", "revision:5"); !errors.Is(err, ErrInvalidOneTimeGrant) {
		t.Fatalf("重复消费未拒绝: %v", err)
	}

	expired, _, err := store.Issue("desktop-session", "claude.restore", "revision:5")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := store.Consume(expired, "desktop-session", "claude.restore", "revision:5"); !errors.Is(err, ErrInvalidOneTimeGrant) {
		t.Fatalf("过期授权未拒绝: %v", err)
	}
}

func TestOneTimeGrantBoundsOutstandingEntries(t *testing.T) {
	store, err := NewOneTimeGrantStore(OneTimeGrantOptions{TTL: time.Minute, Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Issue("desktop-session", "claude.takeover", "revision:5"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Issue("desktop-session", "claude.restore", "revision:5"); !errors.Is(err, ErrOneTimeGrantCapacity) {
		t.Fatalf("容量限制未生效: %v", err)
	}
}

func TestOneTimeGrantReplacingRevokesPreviousToken(t *testing.T) {
	store, err := NewOneTimeGrantStore(OneTimeGrantOptions{TTL: time.Minute, Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := store.IssueReplacing("authorization-request-1", "desktop-session", "claude.takeover", "revision:5:mutation-1")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.IssueReplacing("authorization-request-1", "desktop-session", "claude.takeover", "revision:5:mutation-1")
	if err != nil || first == second {
		t.Fatalf("替换签发失败: first=%q second=%q err=%v", first, second, err)
	}
	if err := store.Consume(first, "desktop-session", "claude.takeover", "revision:5:mutation-1"); !errors.Is(err, ErrInvalidOneTimeGrant) {
		t.Fatalf("旧 token 未撤销: %v", err)
	}
	if err := store.Consume(second, "desktop-session", "claude.takeover", "revision:5:mutation-1"); err != nil {
		t.Fatalf("新 token 不可消费: %v", err)
	}
}

func TestOneTimeGrantRevokeSlotInvalidatesCurrentToken(t *testing.T) {
	store, err := NewOneTimeGrantStore(OneTimeGrantOptions{TTL: time.Minute, Capacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.IssueReplacing("authorization-request-2", "desktop-session", "claude.takeover", "revision:5")
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := store.RevokeSlot("authorization-request-2")
	if err != nil || !revoked {
		t.Fatalf("slot 撤销失败: revoked=%v err=%v", revoked, err)
	}
	if err := store.Consume(token, "desktop-session", "claude.takeover", "revision:5"); !errors.Is(err, ErrInvalidOneTimeGrant) {
		t.Fatalf("slot 撤销后 token 仍可消费: %v", err)
	}
	revoked, err = store.RevokeSlot("authorization-request-2")
	if err != nil || revoked {
		t.Fatalf("重复撤销应幂等: revoked=%v err=%v", revoked, err)
	}
}
