package configfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
)

func TestWatcherReloadsValidatedExternalChangeAndPublishes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	registry := NewRegistry()
	if err := registry.Register(Schema{
		ID: "app.settings", Version: 1,
		Validate: func(data []byte) error {
			if !strings.Contains(string(data), "schema_version = 1") {
				return ErrInvalidSchema
			}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	initial := []byte("schema_version = 1\n\n[security]\nmode = \"strict\"\n\n[ui]\ntheme = \"light\"\n")
	store := New(Options{
		Path: path, MaxBytes: 1024, Initial: initial,
		Registry: registry, SchemaID: "app.settings", SchemaVersion: 1,
		LockedKeys: []string{"security.mode"},
	})
	hub := NewHub()
	subscription := hub.Subscribe("settings", 4)
	t.Cleanup(subscription.Cancel)

	watcher, err := NewWatcher(WatchOptions{Store: store, Hub: hub, Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if string(watcher.LastSnapshot().Data) != string(initial) {
		t.Fatalf("初始快照错误: %q", watcher.LastSnapshot().Data)
	}

	updated := []byte("schema_version = 1\n\n[security]\nmode = \"strict\"\n\n[ui]\ntheme = \"dark\"\n")
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := watcher.Poll(); err != nil {
		t.Fatal(err)
	}
	event := <-subscription.C
	if event.Component != "settings" || !strings.Contains(string(event.Snapshot.Data), "theme = \"dark\"") {
		t.Fatalf("热更新事件错误: %#v", event)
	}
	if watcher.LastSnapshot().Revision != Digest(updated) {
		t.Fatalf("热更新后快照未推进: %#v", watcher.LastSnapshot())
	}
}

func TestWatcherRejectsInvalidOrLockedExternalChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	registry := NewRegistry()
	if err := registry.Register(Schema{
		ID: "app.settings", Version: 1,
		Validate: func(data []byte) error {
			if !strings.Contains(string(data), "schema_version = 1") {
				return ErrInvalidSchema
			}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	initial := []byte("schema_version = 1\n\n[security]\nmode = \"strict\"\n")
	store := New(Options{
		Path: path, MaxBytes: 1024, Initial: initial,
		Registry: registry, SchemaID: "app.settings", SchemaVersion: 1,
		LockedKeys: []string{"security.mode"},
	})
	hub := NewHub()
	subscription := hub.Subscribe("settings", 1)
	t.Cleanup(subscription.Cancel)
	watcher, err := NewWatcher(WatchOptions{Store: store, Hub: hub, Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	initialRevision := watcher.LastSnapshot().Revision

	if err := os.WriteFile(path, []byte("schema_version = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := watcher.Poll(); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("非法 schema 热更新错误 = %v", err)
	}
	if err := os.WriteFile(path, []byte("schema_version = 1\n\n[security]\nmode = \"loose\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := watcher.Poll(); !errors.Is(err, ErrPolicyLocked) {
		t.Fatalf("锁定字段热更新错误 = %v", err)
	}
	if watcher.LastSnapshot().Revision != initialRevision {
		t.Fatal("非法外部变更不得替换内存中的有效快照")
	}
	select {
	case event := <-subscription.C:
		t.Fatalf("非法热更新不得发布: %#v", event)
	default:
	}
}

func TestWatcherSkipsUnchangedRevisionAndRollsBackFailedApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	store := New(Options{Path: path, MaxBytes: 128, Initial: []byte("version=1\n")})
	applied := 0
	watcher, err := NewWatcher(WatchOptions{
		Store: store, Interval: time.Second,
		Apply: func(snapshot Snapshot) error {
			applied++
			if strings.Contains(string(snapshot.Data), "version=2") {
				return errors.New("apply failed")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := watcher.Poll(); err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Fatalf("相同 revision 不得重复 apply: %d", applied)
	}
	if err := os.WriteFile(path, []byte("version=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := watcher.Poll(); err == nil || !strings.Contains(err.Error(), "apply failed") {
		t.Fatalf("apply 失败错误 = %v", err)
	}
	if string(watcher.LastSnapshot().Data) != "version=1\n" {
		t.Fatalf("apply 失败必须保留上一份有效快照: %q", watcher.LastSnapshot().Data)
	}
}

func TestWatcherStartPollsOnClockTicks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	store := New(Options{Path: path, MaxBytes: 128, Initial: []byte("version=1\n")})
	hub := NewHub()
	subscription := hub.Subscribe("runtime", 2)
	t.Cleanup(subscription.Cancel)
	manual := testkit.NewManualClock(time.Unix(1, 0).UTC())
	watcher, err := NewWatcher(WatchOptions{Store: store, Hub: hub, Clock: manual, Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := watcher.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("version=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manual.Advance(time.Second)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-subscription.C:
			if strings.Contains(string(event.Snapshot.Data), "version=2") {
				watcher.Stop()
				return
			}
		case <-deadline:
			t.Fatal("时钟推进后未收到热更新")
		}
	}
}

func TestNewWatcherRejectsInvalidOptions(t *testing.T) {
	if _, err := NewWatcher(WatchOptions{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("空参数错误 = %v", err)
	}
}
