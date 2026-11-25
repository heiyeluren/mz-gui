package remoteservice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

func TestHTTPReachabilityProbeClassifiesStatusAndRejectsRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ok":
			if request.Method != http.MethodHead {
				t.Errorf("默认探测方法应为 HEAD，实际 %s", request.Method)
			}
			writer.WriteHeader(http.StatusNoContent)
		case "/fail":
			writer.WriteHeader(http.StatusServiceUnavailable)
		case "/redirect":
			http.Redirect(writer, request, "/ok", http.StatusFound)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	probe, err := NewHTTPReachabilityProbe(HTTPReachabilityProbeOptions{
		URL: server.URL + "/ok", Timeout: time.Second,
		Clock: reachabilityFixedClock{now: time.Date(2026, 8, 16, 11, 30, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatal(err)
	}
	sample, err := probe.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sample.Online || sample.StatusCode != http.StatusNoContent || sample.CheckedAt.IsZero() || sample.Latency <= 0 {
		t.Fatalf("在线样本异常: %#v", sample)
	}

	probe, err = NewHTTPReachabilityProbe(HTTPReachabilityProbeOptions{URL: server.URL + "/fail", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	sample, err = probe.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sample.Online || sample.StatusCode != http.StatusServiceUnavailable || sample.Reason != "remote.status_unhealthy" {
		t.Fatalf("失败样本异常: %#v", sample)
	}

	probe, err = NewHTTPReachabilityProbe(HTTPReachabilityProbeOptions{URL: server.URL + "/redirect", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	sample, err = probe.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sample.Online || sample.Reason != "remote.redirect_blocked" {
		t.Fatalf("重定向必须被拒绝: %#v", sample)
	}
}

func TestHTTPReachabilityProbeRejectsInvalidConfigAndReportsNetworkError(t *testing.T) {
	if _, err := NewHTTPReachabilityProbe(HTTPReachabilityProbeOptions{URL: "file:///tmp/a"}); !errors.Is(err, ErrInvalidReachabilityProbe) {
		t.Fatalf("非法 scheme 必须拒绝: %v", err)
	}
	if _, err := NewHTTPReachabilityProbe(HTTPReachabilityProbeOptions{URL: "https://example.test", Method: http.MethodPost}); !errors.Is(err, ErrInvalidReachabilityProbe) {
		t.Fatalf("非法 method 必须拒绝: %v", err)
	}
	probe, err := NewHTTPReachabilityProbe(HTTPReachabilityProbeOptions{URL: "http://127.0.0.1:1/health", Timeout: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	sample, err := probe.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sample.Online || sample.Reason != "remote.network_error" {
		t.Fatalf("网络错误样本异常: %#v", sample)
	}
}

func TestReachabilityStateUsesThresholdsForOfflineAndRecovery(t *testing.T) {
	state, err := NewReachabilityState(ReachabilityPolicy{OfflineAfterFailures: 2, OnlineAfterSuccesses: 2})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := state.Snapshot(); snapshot.Status != ReachabilityUnknown {
		t.Fatalf("初始状态必须 unknown: %#v", snapshot)
	}

	firstFailure := state.Record(ReachabilitySample{Online: false, CheckedAt: time.Unix(1, 0).UTC(), Reason: "remote.network_error"})
	if firstFailure.Status != ReachabilityUnknown || firstFailure.FailureStreak != 1 {
		t.Fatalf("首次失败不应立刻 offline: %#v", firstFailure)
	}
	secondFailure := state.Record(ReachabilitySample{Online: false, CheckedAt: time.Unix(2, 0).UTC(), Reason: "remote.network_error"})
	if secondFailure.Status != ReachabilityOffline || !secondFailure.Changed {
		t.Fatalf("连续失败后应 offline: %#v", secondFailure)
	}
	firstSuccess := state.Record(ReachabilitySample{Online: true, CheckedAt: time.Unix(3, 0).UTC(), StatusCode: http.StatusNoContent})
	if firstSuccess.Status != ReachabilityOffline || firstSuccess.SuccessStreak != 1 {
		t.Fatalf("首次成功不应立刻恢复 online: %#v", firstSuccess)
	}
	secondSuccess := state.Record(ReachabilitySample{Online: true, CheckedAt: time.Unix(4, 0).UTC(), StatusCode: http.StatusNoContent})
	if secondSuccess.Status != ReachabilityOnline || !secondSuccess.Changed || secondSuccess.RecoveredAt.IsZero() {
		t.Fatalf("连续成功后应恢复 online: %#v", secondSuccess)
	}
}

type reachabilityFixedClock struct {
	now time.Time
}

func (clock reachabilityFixedClock) Now() time.Time {
	return clock.now
}

func (clock reachabilityFixedClock) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- clock.now
	close(ch)
	return ch
}

func (clock reachabilityFixedClock) NewTicker(duration time.Duration) (frameworkclock.Ticker, error) {
	return nil, frameworkclock.ErrInvalidDuration
}
