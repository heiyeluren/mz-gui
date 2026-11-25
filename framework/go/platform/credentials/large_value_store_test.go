package credentials

import (
	"context"
	"encoding/base64"
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"
)

const largeValueTestKey = "ultra-fixture/feishu/" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// 保留真实 Store 参数校验，替换的只是系统调用，禁止测试接触用户 Keychain。
type largeValueTestBackend struct {
	mu     sync.Mutex
	values map[string]string
	calls  int
	before func(string, string, string) error
	after  func(string, string, string) error
}

func (b *largeValueTestBackend) run(op, key, value string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	if b.before != nil {
		if err := b.before(op, key, value); err != nil {
			return "", err
		}
	}
	switch op {
	case "get":
		var ok bool
		value, ok = b.values[key]
		if !ok {
			return "", ErrNotFound
		}
	case "set":
		if len(value) > 2048 || len(key) > 128 {
			return "", errors.New("production capacity exceeded")
		}
		// macOS go-keyring 会 Base64 整个值，再检查交互命令的 4096 字节上限。
		command := "add-generic-password -U -s com.mz.mzgui.credentials -a " + key + " -w go-keyring-base64:" + base64.StdEncoding.EncodeToString([]byte(value)) + "\n"
		if len(command) > 4096 {
			return "", errors.New("macOS command capacity exceeded")
		}
		b.values[key] = value
	case "delete":
		if _, ok := b.values[key]; !ok {
			return "", ErrNotFound
		}
		delete(b.values, key)
	}
	if b.after != nil {
		if err := b.after(op, key, value); err != nil {
			return "", err
		}
	}
	return value, nil
}

func (b *largeValueTestBackend) Get(_ string, key string) (string, error) {
	return b.run("get", key, "")
}
func (b *largeValueTestBackend) Set(_ string, key, value string) error {
	_, err := b.run("set", key, value)
	return err
}
func (b *largeValueTestBackend) Delete(_ string, key string) error {
	_, err := b.run("delete", key, "")
	return err
}
func (b *largeValueTestBackend) snapshot() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return maps.Clone(b.values)
}

func largeValueFixture(t *testing.T) (*LargeValueStore, *Store, *largeValueTestBackend) {
	t.Helper()
	b := &largeValueTestBackend{values: make(map[string]string)}
	raw, err := New(Options{Backend: b, Service: "com.mz.mzgui.credentials"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewLargeValueStore(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s, raw, b
}

func requireLargeValue(t *testing.T, s *LargeValueStore, want string) {
	t.Helper()
	got, err := s.Get(context.Background(), largeValueTestKey)
	if err != nil || got != want {
		t.Fatal("credential round trip failed", err)
	}
}

func TestLargeValueStoreProductionContractAndRestart(t *testing.T) {
	for name, value := range map[string]string{
		"small":            "fixture-value",
		"control_and_utf8": "凭据\n第二行\x00",
		"reserved_prefix":  "mz-large-value:v1:literal-user-data",
		"utf8_over_128k":   strings.Repeat("跨🙂abc", 16000),
		"maximum":          strings.Repeat("x", 256<<10),
	} {
		t.Run(name, func(t *testing.T) {
			s, raw, b := largeValueFixture(t)
			if err := s.Set(context.Background(), largeValueTestKey, value); err != nil {
				t.Fatal("save failed", err)
			}
			restored, err := NewLargeValueStore(raw)
			if err != nil {
				t.Fatal(err)
			}
			requireLargeValue(t, restored, value)
			if err := restored.Delete(context.Background(), largeValueTestKey); err != nil {
				t.Fatal(err)
			}
			if len(b.snapshot()) != 0 {
				t.Fatal("delete left credential records behind")
			}
		})
	}
}

func TestLargeValueStoreReadsAndReplacesLegacyRecords(t *testing.T) {
	s, raw, b := largeValueFixture(t)
	ctx := context.Background()
	if err := raw.Set(ctx, largeValueTestKey, "legacy-fixture"); err != nil {
		t.Fatal(err)
	}
	requireLargeValue(t, s, "legacy-fixture")
	for _, value := range []string{strings.Repeat("new", 3000), "small-again"} {
		if err := s.Set(ctx, largeValueTestKey, value); err != nil {
			t.Fatal(err)
		}
		requireLargeValue(t, s, value)
	}
	if err := s.Delete(ctx, largeValueTestKey); err != nil {
		t.Fatal(err)
	}
	if len(b.snapshot()) != 0 {
		t.Fatal("overwrite or delete leaked old records")
	}
}

func TestLargeValueStoreRejectsInvalidInputBeforeBackend(t *testing.T) {
	if _, err := NewLargeValueStore(nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("nil store accepted")
	}
	s, _, b := largeValueFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		ctx        context.Context
		key, value string
	}{
		{nil, largeValueTestKey, "value"},
		{ctx, largeValueTestKey, "value"},
		{context.Background(), "Invalid/key", "value"},
		{context.Background(), largeValueTestKey, ""},
		{context.Background(), largeValueTestKey, string([]byte{0xff})},
		{context.Background(), largeValueTestKey, strings.Repeat("x", (256<<10)+1)},
	} {
		if err := s.Set(test.ctx, test.key, test.value); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	if b.calls != 0 {
		t.Fatal("invalid input reached the backend")
	}
}

func TestLargeValueStoreDetectsCorruption(t *testing.T) {
	for _, mode := range []string{"missing_chunk", "altered_chunk", "truncated_manifest", "unsupported_version"} {
		t.Run(mode, func(t *testing.T) {
			s, _, b := largeValueFixture(t)
			if err := s.Set(context.Background(), largeValueTestKey, strings.Repeat("secret", 2000)); err != nil {
				t.Fatal(err)
			}
			b.mu.Lock()
			if mode == "truncated_manifest" {
				b.values[largeValueTestKey] = b.values[largeValueTestKey][:30]
			} else if mode == "unsupported_version" {
				b.values[largeValueTestKey] = strings.Replace(b.values[largeValueTestKey], ":v1:", ":v9:", 1)
			} else {
				for key, value := range b.values {
					if key == largeValueTestKey {
						continue
					}
					if mode == "missing_chunk" {
						delete(b.values, key)
					} else {
						b.values[key] = "A" + value[1:]
					}
					break
				}
			}
			b.mu.Unlock()
			if value, err := s.Get(context.Background(), largeValueTestKey); err == nil || value != "" {
				t.Fatal("corrupt credential was returned")
			}
		})
	}
}

func TestLargeValueStoreIncompleteWriteAndCancellationPreservePriorValue(t *testing.T) {
	for _, cancelWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial_failure", true: "cancelled"}[cancelWrite], func(t *testing.T) {
			s, _, b := largeValueFixture(t)
			prior := strings.Repeat("old", 2000)
			if err := s.Set(context.Background(), largeValueTestKey, prior); err != nil {
				t.Fatal(err)
			}
			before := b.snapshot()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writes := 0
			b.after = func(op, key, _ string) error {
				if op == "set" && key != largeValueTestKey {
					writes++
					if writes == 2 {
						if cancelWrite {
							cancel()
						} else {
							return errors.New("fixture-private-error")
						}
					}
				}
				return nil
			}
			err := s.Set(ctx, largeValueTestKey, strings.Repeat("next", 2500))
			if err == nil || strings.Contains(err.Error(), "fixture-private-error") {
				t.Fatal("failure was hidden or leaked backend details")
			}
			b.after = nil
			requireLargeValue(t, s, prior)
			if !maps.Equal(before, b.snapshot()) {
				t.Fatal("uncommitted partial write changed prior records or leaked chunks")
			}
		})
	}
}

func TestLargeValueStoreAmbiguousCommitKeepsPotentiallyReferencedChunks(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "not_committed", true: "committed"}[committed], func(t *testing.T) {
			s, raw, b := largeValueFixture(t)
			old, next := strings.Repeat("old", 2000), strings.Repeat("new", 3000)
			if err := s.Set(context.Background(), largeValueTestKey, old); err != nil {
				t.Fatal(err)
			}
			fail := func(op, key, _ string) error {
				if op == "set" && key == largeValueTestKey {
					return errors.New("unknown commit outcome")
				}
				return nil
			}
			if committed {
				b.after = fail
			} else {
				b.before = fail
			}
			if err := s.Set(context.Background(), largeValueTestKey, next); err == nil {
				t.Fatal("ambiguous commit reported success")
			}
			b.before, b.after = nil, nil
			recovered, _ := NewLargeValueStore(raw)
			want := old
			if committed {
				want = next
			}
			requireLargeValue(t, recovered, want)
			if err := recovered.Delete(context.Background(), largeValueTestKey); err != nil {
				t.Fatal(err)
			}
			if committed && len(b.snapshot()) != 0 {
				t.Fatal("committed recovery did not clean both generations")
			}
		})
	}
}

func TestLargeValueStoreDeleteCanResumeAfterEveryBoundary(t *testing.T) {
	for _, mode := range []string{"before_tombstone", "after_tombstone", "chunk_delete", "after_root_delete"} {
		t.Run(mode, func(t *testing.T) {
			s, raw, b := largeValueFixture(t)
			value := strings.Repeat("value", 1800)
			if err := s.Set(context.Background(), largeValueTestKey, value); err != nil {
				t.Fatal(err)
			}
			fail := func(op, key, _ string) error {
				if ((mode == "before_tombstone" || mode == "after_tombstone") && op == "set" && key == largeValueTestKey) || (mode == "chunk_delete" && op == "delete" && key != largeValueTestKey) || (mode == "after_root_delete" && op == "delete" && key == largeValueTestKey) {
					return errors.New("interrupted delete")
				}
				return nil
			}
			if mode == "after_tombstone" || mode == "after_root_delete" {
				b.after = fail
			} else {
				b.before = fail
			}
			if err := s.Delete(context.Background(), largeValueTestKey); err == nil {
				t.Fatal("failed delete reported success")
			}
			b.before, b.after = nil, nil
			recovered, _ := NewLargeValueStore(raw)
			if mode == "before_tombstone" {
				requireLargeValue(t, recovered, value)
			} else if _, err := recovered.Get(context.Background(), largeValueTestKey); !errors.Is(err, ErrNotFound) {
				t.Fatal("deleted credential became visible")
			}
			if err := recovered.Delete(context.Background(), largeValueTestKey); err != nil && !errors.Is(err, ErrNotFound) {
				t.Fatal(err)
			}
			if len(b.snapshot()) != 0 {
				t.Fatal("resumed delete left shards behind")
			}
		})
	}
}

func TestLargeValueStoreOverwriteCleanupFailureCanResume(t *testing.T) {
	s, raw, b := largeValueFixture(t)
	ctx := context.Background()
	if err := s.Set(ctx, largeValueTestKey, strings.Repeat("old", 2000)); err != nil {
		t.Fatal(err)
	}
	b.before = func(op, key, _ string) error {
		if op == "delete" && key != largeValueTestKey {
			return errors.New("cleanup failed")
		}
		return nil
	}
	next := strings.Repeat("next", 2000)
	if err := s.Set(ctx, largeValueTestKey, next); err == nil {
		t.Fatal("cleanup failure was hidden")
	}
	b.before = nil
	recovered, _ := NewLargeValueStore(raw)
	requireLargeValue(t, recovered, next)
	if err := recovered.Set(ctx, largeValueTestKey, "final"); err != nil {
		t.Fatal(err)
	}
	if err := recovered.Delete(ctx, largeValueTestKey); err != nil {
		t.Fatal(err)
	}
	if len(b.snapshot()) != 0 {
		t.Fatal("cleanup recovery leaked retired shards")
	}
}

func TestLargeValueStoreConcurrentWrappers(t *testing.T) {
	s, raw, b := largeValueFixture(t)
	ctx := context.Background()
	if err := s.Set(ctx, largeValueTestKey, strings.Repeat("a", 6000)); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			other, _ := NewLargeValueStore(raw)
			for j := 0; j < 3; j++ {
				if err := other.Set(ctx, largeValueTestKey, strings.Repeat(string(rune('a'+i)), 6000+i)); err != nil {
					t.Error("concurrent write failed", err)
					return
				}
				if value, err := other.Get(ctx, largeValueTestKey); err != nil || len(value) < 6000 {
					t.Error("concurrent read failed", err)
					return
				}
			}
		}(i)
	}
	group.Wait()
	if err := s.Delete(ctx, largeValueTestKey); err != nil {
		t.Fatal(err)
	}
	if len(b.snapshot()) != 0 {
		t.Fatal("concurrent wrappers leaked generations")
	}
}

func TestLargeValueStoreMaximumKeysRemainIsolated(t *testing.T) {
	s, _, b := largeValueFixture(t)
	ctx := context.Background()
	left, right := "a/"+strings.Repeat("x", 126), "b/"+strings.Repeat("x", 126)
	for _, key := range []string{left, right} {
		if err := s.Set(ctx, key, strings.Repeat(key[:1], 7000)); err != nil {
			t.Fatal("maximum logical key failed", err)
		}
	}
	if err := s.Delete(ctx, left); err != nil {
		t.Fatal(err)
	}
	if value, err := s.Get(ctx, right); err != nil || value != strings.Repeat("b", 7000) {
		t.Fatal("one logical key modified another credential")
	}
	if err := s.Delete(ctx, right); err != nil {
		t.Fatal(err)
	}
	if len(b.snapshot()) != 0 {
		t.Fatal("maximum-key cleanup leaked shards")
	}
}

func TestLargeValueStoreCancelledReadAndDeleteRecover(t *testing.T) {
	for _, operation := range []string{"get", "delete"} {
		t.Run(operation, func(t *testing.T) {
			s, raw, b := largeValueFixture(t)
			value := strings.Repeat("fixture", 2000)
			if err := s.Set(context.Background(), largeValueTestKey, value); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b.after = func(op, key, _ string) error {
				if op == operation && key != largeValueTestKey {
					cancel()
				}
				return nil
			}
			if operation == "get" {
				if got, err := s.Get(ctx, largeValueTestKey); !errors.Is(err, context.Canceled) || got != "" {
					t.Fatal("cancelled read returned partial credential")
				}
			} else if err := s.Delete(ctx, largeValueTestKey); !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled delete reported success")
			}
			b.after = nil
			recovered, _ := NewLargeValueStore(raw)
			if operation == "get" {
				requireLargeValue(t, recovered, value)
			} else if _, err := recovered.Get(context.Background(), largeValueTestKey); !errors.Is(err, ErrNotFound) {
				t.Fatal("partly deleted credential became visible")
			}
			if err := recovered.Delete(context.Background(), largeValueTestKey); err != nil {
				t.Fatal(err)
			}
			if len(b.snapshot()) != 0 {
				t.Fatal("cancellation recovery leaked shards")
			}
		})
	}
}
