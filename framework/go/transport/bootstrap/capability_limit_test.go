package bootstrap

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	securitysession "github.com/heiyeluren/mz-gui/framework/go/security/session"
)

func numberedCapabilities(count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = fmt.Sprintf("module.c%d", index)
	}
	return values
}

// 角色策略、握手授予与客户端探针共用同一个上限，避免一处放行、另一处拒绝。
func TestRoleCapabilityLimitIsSharedAcrossHandshake(t *testing.T) {
	now := time.Now().UTC()
	config := func(count int) Config {
		return Config{ProtocolMajor: ProtocolMajor, ProtocolMinor: ProtocolMinor, Server: serverIdentity(),
			Roles:    map[string]RolePolicy{"desktop": {ProcessName: "mz-gui-desktop", Capabilities: numberedCapabilities(count)}},
			Sessions: newSessionStore(t, &now), Now: func() time.Time { return now }, Random: bytes.NewReader(bytes.Repeat([]byte{1}, 128)),
			MaxClockSkew: 2 * time.Minute, NonceTTL: 5 * time.Minute, MaxNonces: 64}
	}
	for _, count := range []int{65, securitysession.MaxCapabilities} {
		if _, err := New(config(count)); err != nil {
			t.Fatalf("%d 项能力的角色应可配置: %v", count, err)
		}
		allowed := map[string]struct{}{}
		for _, value := range numberedCapabilities(count) {
			allowed[value] = struct{}{}
		}
		if granted, ok := grantCapabilities(numberedCapabilities(count), allowed); !ok || len(granted) != count {
			t.Fatalf("%d 项能力应可授予", count)
		}
	}
	over := securitysession.MaxCapabilities + 1
	if _, err := New(config(over)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("超过上限的角色必须拒绝: %v", err)
	}
	if _, ok := grantCapabilities(numberedCapabilities(over), map[string]struct{}{}); ok {
		t.Fatal("超过上限的请求必须拒绝")
	}
}
