package session

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func numberedCapabilities(count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = fmt.Sprintf("module.c%d", index)
	}
	return values
}

// 能力上限是防御性约束：需要容纳已超过 64 项的真实角色，同时仍拒绝超限列表。
func TestSessionStoreCapabilityLimit(t *testing.T) {
	if MaxCapabilities < 128 {
		t.Fatalf("能力上限不得低于 128，实际为 %d", MaxCapabilities)
	}
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, 4)
	for _, count := range []int{65, MaxCapabilities} {
		session, err := store.Issue("desktop", "1.2.3", numberedCapabilities(count))
		if err != nil || len(session.Capabilities()) != count {
			t.Fatalf("%d 项能力应可签发: %v", count, err)
		}
	}
	if _, err := store.Issue("desktop", "1.2.3", numberedCapabilities(MaxCapabilities+1)); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("超过上限必须拒绝: %v", err)
	}
}
