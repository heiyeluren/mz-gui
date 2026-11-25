package session

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionStoreIssuesValidatesAndCopiesCapabilities(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, 4)
	session, err := store.Issue("desktop", "1.2.3", []string{"task.write", "task.read", "task.read"})
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != "session-1" || len(session.Token) != 64 || !session.ExpiresAt.Equal(now.Add(15*time.Minute)) || !session.Has("task.write") {
		t.Fatalf("签发会话错误: %#v", session)
	}
	if _, err := store.ValidateCredential(session.ID, session.Token); err != nil {
		t.Fatalf("会话凭据未通过校验: %v", err)
	}
	if _, err := store.ValidateCredential(session.ID, strings.Repeat("0", 64)); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("错误 token 未被拒绝: %v", err)
	}
	capabilities := session.Capabilities()
	capabilities[0] = "changed"
	validated, err := store.Validate(session.ID)
	if err != nil || validated.Has("changed") || !validated.Has("task.read") {
		t.Fatalf("会话副本污染存储: session=%#v err=%v", validated, err)
	}
	if validated.Token != "" {
		t.Fatal("仅凭会话 ID 查询不得返回 token")
	}
	credential, err := store.ValidateCredential(session.ID, session.Token)
	if err != nil || credential.Token != session.Token {
		t.Fatalf("凭据校验仍应返回 token: %#v err=%v", credential, err)
	}
}

func TestSessionStoreExpiresRevokesAndPrunes(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, 1)
	first, err := store.Issue("desktop", "1.0.0", []string{"task.read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Issue("desktop", "1.0.0", nil); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("容量限制错误 = %v", err)
	}
	now = now.Add(15 * time.Minute)
	if _, err := store.Validate(first.ID); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("过期错误 = %v", err)
	}
	second, err := store.Issue("desktop", "1.0.0", nil)
	if err != nil {
		t.Fatalf("过期清理后未释放容量: %v", err)
	}
	store.Revoke(second.ID)
	if _, err := store.Validate(second.ID); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("撤销错误 = %v", err)
	}
}

func TestSessionStoreIsConcurrencySafe(t *testing.T) {
	now := time.Now().UTC()
	var sequence int
	var idMu sync.Mutex
	store, err := NewSessionStore(StoreOptions{
		TTL: 15 * time.Minute, MaxSessions: 64, Now: func() time.Time { return now },
		NewID: func() (string, error) {
			idMu.Lock()
			defer idMu.Unlock()
			sequence++
			return fmt.Sprintf("session-%d", sequence), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errorsSeen := make(chan error, 32)
	for index := 0; index < 32; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			session, issueErr := store.Issue("desktop", "1.0.0", []string{"task.read"})
			if issueErr != nil {
				errorsSeen <- issueErr
				return
			}
			if _, validateErr := store.Validate(session.ID); validateErr != nil {
				errorsSeen <- validateErr
			}
		}()
	}
	group.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("并发会话错误: %v", err)
	}
}

func TestSessionStoreRejectsInvalidConfigurationAndInput(t *testing.T) {
	if _, err := NewSessionStore(StoreOptions{}); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("非法选项错误 = %v", err)
	}
	for _, options := range []StoreOptions{
		{TTL: 15 * time.Minute, MaxLifetime: 10 * time.Minute, MaxSessions: 1},
		{TTL: 15 * time.Minute, MaxLifetime: 8 * 24 * time.Hour, MaxSessions: 1},
		{TTL: 15 * time.Minute, MaxSessions: 1, EventBuffer: 4097},
	} {
		if _, err := NewSessionStore(options); !errors.Is(err, ErrInvalidSession) {
			t.Fatalf("非法扩展选项未拒绝: %#v err=%v", options, err)
		}
	}
	if _, err := NewSessionStore(StoreOptions{TTL: 24 * time.Hour, MaxSessions: 1}); err != nil {
		t.Fatalf("24 小时 TTL 的默认绝对期限应收敛到 7 天: %v", err)
	}
	now := time.Now().UTC()
	store := newTestStore(t, &now, 2)
	for _, input := range []struct {
		component string
		version   string
		caps      []string
	}{
		{version: "1.0.0"}, {component: "desktop"}, {component: "desktop", version: "1.0.0", caps: []string{""}},
	} {
		if _, err := store.Issue(input.component, input.version, input.caps); !errors.Is(err, ErrInvalidSession) {
			t.Fatalf("非法输入错误 = %v", err)
		}
	}
	issued, err := store.Issue("desktop", "1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RenewCredential(issued.ID, strings.Repeat("0", 64)); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("错误 token 续签未拒绝: %v", err)
	}
	if store.RevokeCredential(issued.ID, issued.Token, "arbitrary_reason") {
		t.Fatal("任意撤销 reason 不应被接受")
	}
}

func TestSessionStoreRenewsWithTokenRotationAndAbsoluteLifetime(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sequence := 0
	store, err := NewSessionStore(StoreOptions{
		TTL: 15 * time.Minute, MaxLifetime: 40 * time.Minute, MaxSessions: 2, Now: func() time.Time { return now },
		NewID: func() (string, error) { return "session-renew", nil },
		NewToken: func() (string, error) {
			sequence++
			return fmt.Sprintf("%064d", sequence), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := store.Issue("desktop", "1.0.0", []string{"task.read"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	renewed, err := store.RenewCredential(issued.ID, issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Token == issued.Token || !renewed.ExpiresAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("续签未轮换 token 或期限错误: issued=%#v renewed=%#v", issued, renewed)
	}
	if _, err := store.ValidateCredential(issued.ID, issued.Token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("旧 token 续签后仍有效: %v", err)
	}
	now = issued.IssuedAt.Add(20 * time.Minute)
	secondRenewal, err := store.RenewCredential(renewed.ID, renewed.Token)
	if err != nil {
		t.Fatal(err)
	}
	now = issued.IssuedAt.Add(30 * time.Minute)
	final, err := store.RenewCredential(secondRenewal.ID, secondRenewal.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !final.ExpiresAt.Equal(issued.IssuedAt.Add(40 * time.Minute)) {
		t.Fatalf("续签越过绝对期限: %s", final.ExpiresAt)
	}
	now = final.ExpiresAt
	if _, err := store.RenewCredential(final.ID, final.Token); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("达到绝对期限后仍可续签: %v", err)
	}
}

func TestSessionStoreBroadcastsRevocationAndExpiryWithoutToken(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, 2)
	subscription, err := store.SubscribeEvents()
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Cancel()
	issued, err := store.Issue("desktop", "1.0.0", []string{"task.read"})
	if err != nil {
		t.Fatal(err)
	}
	issueEvent := <-subscription.Events()
	if issueEvent.Kind != EventIssued || issueEvent.Sequence != 1 || issueEvent.SessionID != issued.ID {
		t.Fatalf("签发事件错误: %#v", issueEvent)
	}
	if !store.RevokeCredential(issued.ID, issued.Token, RevokeReasonClientShutdown) {
		t.Fatal("有效凭据撤销失败")
	}
	revoked := <-subscription.Events()
	if revoked.Kind != EventRevoked || revoked.Sequence != 2 || revoked.Reason != RevokeReasonClientShutdown || strings.Contains(fmt.Sprintf("%#v", revoked), issued.Token) {
		t.Fatalf("撤销事件错误或泄露 token: %#v", revoked)
	}
	second, err := store.Issue("browser-host", "1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	<-subscription.Events()
	now = second.ExpiresAt
	if _, err := store.Validate(second.ID); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("过期会话错误: %v", err)
	}
	expired := <-subscription.Events()
	if expired.Kind != EventExpired || expired.Sequence != 4 || expired.SessionID != second.ID {
		t.Fatalf("过期事件错误: %#v", expired)
	}
}

func newTestStore(t *testing.T, now *time.Time, maximum int) *SessionStore {
	t.Helper()
	sequence := 0
	store, err := NewSessionStore(StoreOptions{
		TTL: 15 * time.Minute, MaxSessions: maximum, Now: func() time.Time { return *now },
		NewID: func() (string, error) {
			sequence++
			return fmt.Sprintf("session-%d", sequence), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}
