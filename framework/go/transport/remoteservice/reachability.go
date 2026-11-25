package remoteservice

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

type ReachabilityStatus string

const (
	ReachabilityUnknown ReachabilityStatus = "unknown"
	ReachabilityOnline  ReachabilityStatus = "online"
	ReachabilityOffline ReachabilityStatus = "offline"
)

type ReachabilitySample struct {
	Online     bool
	CheckedAt  time.Time
	Latency    time.Duration
	StatusCode int
	Reason     string
	Error      string
}

type ReachabilityPolicy struct {
	OfflineAfterFailures int
	OnlineAfterSuccesses int
}

type ReachabilitySnapshot struct {
	Status        ReachabilityStatus
	LastSample    ReachabilitySample
	SuccessStreak int
	FailureStreak int
	Changed       bool
	LastChangedAt time.Time
	RecoveredAt   time.Time
	Reason        string
}

type ReachabilityState struct {
	mu       sync.Mutex
	policy   ReachabilityPolicy
	snapshot ReachabilitySnapshot
}

func NewReachabilityState(policy ReachabilityPolicy) (*ReachabilityState, error) {
	if policy.OfflineAfterFailures < 1 {
		policy.OfflineAfterFailures = 1
	}
	if policy.OnlineAfterSuccesses < 1 {
		policy.OnlineAfterSuccesses = 1
	}
	return &ReachabilityState{
		policy: policy,
		snapshot: ReachabilitySnapshot{
			Status: ReachabilityUnknown,
		},
	}, nil
}

func (state *ReachabilityState) Record(sample ReachabilitySample) ReachabilitySnapshot {
	if state == nil {
		return ReachabilitySnapshot{}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if sample.CheckedAt.IsZero() {
		sample.CheckedAt = time.Now().UTC()
	}
	next := state.snapshot
	next.LastSample = sample
	next.Changed = false
	next.Reason = sample.Reason
	previousStatus := next.Status
	if previousStatus == "" {
		previousStatus = ReachabilityUnknown
		next.Status = ReachabilityUnknown
	}
	if sample.Online {
		next.SuccessStreak++
		next.FailureStreak = 0
		if next.SuccessStreak >= state.policy.OnlineAfterSuccesses {
			next.Status = ReachabilityOnline
		}
	} else {
		next.FailureStreak++
		next.SuccessStreak = 0
		if next.FailureStreak >= state.policy.OfflineAfterFailures {
			next.Status = ReachabilityOffline
		}
	}
	if next.Status != previousStatus {
		next.Changed = true
		next.LastChangedAt = sample.CheckedAt
		if previousStatus == ReachabilityOffline && next.Status == ReachabilityOnline {
			next.RecoveredAt = sample.CheckedAt
		}
	}
	state.snapshot = next
	return next
}

func (state *ReachabilityState) Snapshot() ReachabilitySnapshot {
	if state == nil {
		return ReachabilitySnapshot{}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.snapshot
}

type HTTPReachabilityProbeOptions struct {
	HTTPClient         *http.Client
	URL                string
	Method             string
	Timeout            time.Duration
	AllowedStatusCodes map[int]struct{}
	BypassProxy        bool
	Clock              frameworkclock.Clock
}

type HTTPReachabilityProbe struct {
	client             *http.Client
	rawURL             string
	method             string
	timeout            time.Duration
	allowedStatusCodes map[int]struct{}
	clock              frameworkclock.Clock
}

func NewHTTPReachabilityProbe(options HTTPReachabilityProbeOptions) (*HTTPReachabilityProbe, error) {
	parsed, err := url.Parse(options.URL)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil {
		return nil, ErrInvalidReachabilityProbe
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrInvalidReachabilityProbe
	}
	method := options.Method
	if method == "" {
		method = http.MethodHead
	}
	if method != http.MethodHead && method != http.MethodGet {
		return nil, ErrInvalidReachabilityProbe
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	if options.Clock == nil {
		options.Clock = frameworkclock.System{}
	}
	allowed := cloneStatusSet(options.AllowedStatusCodes)
	if len(allowed) == 0 {
		allowed = successStatusSet()
	}
	client := options.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if options.BypassProxy {
			transport.Proxy = nil
		}
		client = &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &HTTPReachabilityProbe{
		client: client, rawURL: parsed.String(), method: method, timeout: options.Timeout,
		allowedStatusCodes: allowed, clock: options.Clock,
	}, nil
}

func (probe *HTTPReachabilityProbe) Check(ctx context.Context) (ReachabilitySample, error) {
	if probe == nil || ctx == nil {
		return ReachabilitySample{}, ErrInvalidReachabilityProbe
	}
	start := time.Now()
	callContext, cancel := context.WithTimeout(ctx, probe.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callContext, probe.method, probe.rawURL, nil)
	if err != nil {
		return ReachabilitySample{}, err
	}
	response, err := probe.client.Do(request)
	if err != nil {
		return probe.sample(false, 0, start, "remote.network_error", err.Error()), nil
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return probe.sample(false, response.StatusCode, start, "remote.redirect_blocked", ""), nil
	}
	if _, ok := probe.allowedStatusCodes[response.StatusCode]; !ok {
		return probe.sample(false, response.StatusCode, start, "remote.status_unhealthy", ""), nil
	}
	return probe.sample(true, response.StatusCode, start, "", ""), nil
}

func (probe *HTTPReachabilityProbe) sample(online bool, statusCode int, start time.Time, reason, errorText string) ReachabilitySample {
	latency := time.Since(start)
	if latency <= 0 {
		latency = time.Nanosecond
	}
	return ReachabilitySample{
		Online: online, CheckedAt: probe.clock.Now(), Latency: latency,
		StatusCode: statusCode, Reason: reason, Error: errorText,
	}
}

func cloneStatusSet(values map[int]struct{}) map[int]struct{} {
	if len(values) == 0 {
		return nil
	}
	result := make(map[int]struct{}, len(values))
	for value := range values {
		result[value] = struct{}{}
	}
	return result
}

func successStatusSet() map[int]struct{} {
	result := make(map[int]struct{}, 100)
	for status := 200; status < 300; status++ {
		result[status] = struct{}{}
	}
	return result
}
