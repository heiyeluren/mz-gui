package testkit

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/platform/credentials"
)

func TestManualClockTriggersTimersAndTickers(t *testing.T) {
	clock := NewManualClock(time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC))
	timer := clock.After(time.Second)
	ticker, err := clock.NewTicker(500 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer ticker.Stop()
	clock.Advance(500 * time.Millisecond)
	if got := <-ticker.C(); !got.Equal(time.Date(2026, 8, 15, 12, 0, 0, 500*int(time.Millisecond), time.UTC)) {
		t.Fatalf("ticker 时间 = %v", got)
	}
	clock.Advance(500 * time.Millisecond)
	if got := <-timer; !got.Equal(clock.Now()) {
		t.Fatalf("timer 时间 = %v now=%v", got, clock.Now())
	}
}

func TestMemoryCredentialBackendWorksWithStore(t *testing.T) {
	store, err := credentials.New(credentials.Options{Backend: NewMemoryCredentialBackend(), Service: "com.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Set(ctx, "provider/token", "secret-value"); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get(ctx, "provider/token")
	if err != nil || value != "secret-value" {
		t.Fatalf("凭据读取 = %q err=%v", value, err)
	}
	if err := store.Delete(ctx, "provider/token"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "provider/token"); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatalf("删除后错误 = %v", err)
	}
}

func TestFakeRoundTripperRecordsRequests(t *testing.T) {
	transport := NewFakeRoundTripper(HTTPFixture{StatusCode: http.StatusAccepted, Body: []byte("ok")})
	client := &http.Client{Transport: transport}
	request, err := http.NewRequest(http.MethodPost, "https://api.example.test/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("状态码 = %d", response.StatusCode)
	}
	if requests := transport.Requests(); len(requests) != 1 || requests[0].Method != http.MethodPost {
		t.Fatalf("记录请求异常: %#v", requests)
	}
}

func TestFaultInjectorFailsLimitedTimes(t *testing.T) {
	injector := NewFaultInjector()
	injector.Fail("network.fetch", 2, nil)
	if !errors.Is(injector.Check("network.fetch"), ErrFaultInjected) {
		t.Fatal("第一次应注入故障")
	}
	if !errors.Is(injector.Check("network.fetch"), ErrFaultInjected) {
		t.Fatal("第二次应注入故障")
	}
	if err := injector.Check("network.fetch"); err != nil {
		t.Fatalf("第三次不应注入故障: %v", err)
	}
}

func TestFakeProcessPlatformAndWebViewBridge(t *testing.T) {
	process := NewFakeProcess(42)
	process.AppendOutput("ready")
	process.Exit(7)
	if process.Running() || process.ExitCode() != 7 || process.Output()[0] != "ready" {
		t.Fatalf("进程状态异常")
	}
	platform := FakePlatform{OS: "darwin", Arch: "arm64", Permissions: map[string]bool{"notifications": true}, Features: map[string]bool{"webview": true}}
	if !platform.HasPermission("notifications") || !platform.HasFeature("webview") || platform.HasPermission("camera") {
		t.Fatalf("平台假件异常")
	}
	bridge := NewFakeWebViewBridge()
	bridge.Emit("app.ready", "{}")
	bridge.SetEvalResult("window.state", "ok")
	if bridge.Eval("window.state") != "ok" || len(bridge.Events()) != 1 || len(bridge.Scripts()) != 1 {
		t.Fatalf("WebView bridge 异常")
	}
}
