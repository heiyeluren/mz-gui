package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
	frameworkhealth "github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/interaction/windowstate"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/resilience"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestShowcaseServesStylesheetAsCSS(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	application.BundledAssetFileServer(frontendAssets).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Type"), "text/css") || !strings.Contains(recorder.Body.String(), ".app-shell") {
		t.Fatalf("样式资源响应无效: code=%d content-type=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
}

func TestShowcaseUsesFrameworkLifecycleEventsAndCredentialStore(t *testing.T) {
	service, err := NewShowcaseService()
	if err != nil {
		t.Fatal(err)
	}
	service.windowStateDir = t.TempDir()
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Stop(context.Background()) })

	initial, err := service.GetSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !initial.LifecycleReady || !initial.CredentialSet || initial.EventCount != 0 || initial.FrameworkVersion.String() != "1.0.0" || initial.ObservedAt.Time().IsZero() || initial.Health.Status != frameworkhealth.StatusHealthy || len(initial.Health.Checks) != 3 || !initial.Lifecycle.Running || len(initial.Lifecycle.Components) != 2 || initial.CircuitBreaker.State != resilience.StateClosed || initial.RateLimiter.Available != 8 {
		t.Fatalf("初始框架状态不正确: %#v", initial)
	}
	if _, err := service.EmitSample(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, snapshotErr := service.GetSnapshot(context.Background())
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		if snapshot.EventCount == 1 {
			if snapshot.CircuitBreaker.State != resilience.StateClosed || snapshot.RateLimiter.Available != 7 {
				t.Fatalf("Showcase 未消费 Framework 韧性策略: %#v", snapshot)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Showcase EventBus 事件未被消费")
}

func TestShowcasePersistsWindowStateAndRejectsStaleRevision(t *testing.T) {
	service, err := NewShowcaseService()
	if err != nil {
		t.Fatal(err)
	}
	service.windowStateDir = t.TempDir()
	if err := service.persistWindowState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.persistWindowState(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err := windowstate.OpenSQLiteStore(context.Background(), filepath.Join(service.windowStateDir, "window-state.sqlite"), windowstate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Save(context.Background(), windowstate.Snapshot{WindowID: "main", RouteID: "showcase", Revision: 1, Payload: []byte(`{"ready":true}`)}); !errors.Is(err, windowstate.ErrStaleRevision) {
		t.Fatalf("showcase 窗口状态低 revision 必须拒绝: %v", err)
	}
}

func TestShowcaseMemoryWindowStateKeepsMainAndDeletesPreview(t *testing.T) {
	if err := persistMemoryWindowState(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestShowcaseBindsWindowPagesThroughBinder(t *testing.T) {
	store := windowstate.NewMemoryStore(windowstate.Options{})
	if err := bindShowcaseWindowPages(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadPage(context.Background(), "main", "showcase")
	if err != nil || loaded.RouteID != "showcase" || string(loaded.Payload) != `{"ready":true}` {
		t.Fatalf("showcase 页面绑定未落盘: %#v %v", loaded, err)
	}
}

func TestShowcaseExposesFrameworkErrorContract(t *testing.T) {
	service, err := NewShowcaseService()
	if err != nil {
		t.Fatal(err)
	}
	public := service.PreviewFrameworkError(context.Background())
	if public.Code != fault.CodeUnavailable || public.ReasonCode != "showcase.preview_unavailable" || !public.Retryable {
		t.Fatalf("Showcase FrameworkError 异常: %#v", public)
	}
}

func TestShowcaseStopPublishesUnhealthyReadiness(t *testing.T) {
	service, err := NewShowcaseService()
	if err != nil {
		t.Fatal(err)
	}
	service.windowStateDir = t.TempDir()
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.GetSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.LifecycleReady || snapshot.Health.Status != frameworkhealth.StatusUnhealthy || !snapshot.Lifecycle.Closed || snapshot.Lifecycle.Running {
		t.Fatalf("停止后 readiness 未反映真实状态: %#v", snapshot)
	}
}
