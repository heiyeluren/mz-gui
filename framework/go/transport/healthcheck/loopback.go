// Package healthcheck 提供产品无关的本机服务健康探测能力。
package healthcheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidTarget = errors.New("回环健康探测目标无效")
	ErrUnhealthy     = errors.New("本机服务健康检查未通过")
)

type Options struct {
	Timeout time.Duration
	Now     func() time.Time
	// ValidateBody 可选校验有界健康响应；服务协议由调用方解释。
	ValidateBody func([]byte) error
}

type Result struct {
	Endpoint   string
	StatusCode int
	Latency    time.Duration
	CheckedAt  time.Time
}

type LoopbackProbe struct {
	client       *http.Client
	now          func() time.Time
	validateBody func([]byte) error
}

func NewLoopbackProbe(options Options) *LoopbackProbe {
	timeout := options.Timeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 5 * time.Second
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		DisableCompression: true,
	}
	return &LoopbackProbe{
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("健康检查不允许重定向")
			},
		},
		now:          now,
		validateBody: options.ValidateBody,
	}
}

func (probe *LoopbackProbe) Check(ctx context.Context, port int, requestPath string) (Result, error) {
	if ctx == nil || probe == nil || probe.client == nil || port < 1 || port > 65535 || !validPath(requestPath) {
		return Result{}, ErrInvalidTarget
	}
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port) + requestPath
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Result{}, ErrInvalidTarget
	}
	request.Header.Set("Accept", "application/json")
	started := probe.now().UTC()
	response, err := probe.client.Do(request)
	finished := probe.now().UTC()
	if err != nil {
		return Result{Endpoint: endpoint, CheckedAt: finished, Latency: nonNegative(finished.Sub(started))}, fmt.Errorf("%w: %v", ErrUnhealthy, err)
	}
	defer response.Body.Close()
	result := Result{Endpoint: endpoint, StatusCode: response.StatusCode, CheckedAt: finished, Latency: nonNegative(finished.Sub(started))}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf("%w: HTTP %d", ErrUnhealthy, response.StatusCode)
	}
	if probe.validateBody != nil {
		const maximumBodyBytes = 64 << 10
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maximumBodyBytes+1))
		if readErr != nil || len(body) > maximumBodyBytes {
			return result, fmt.Errorf("%w: 健康响应读取失败或过大", ErrUnhealthy)
		}
		if err := probe.validateBody(body); err != nil {
			return result, errors.Join(ErrUnhealthy, err)
		}
	}
	return result, nil
}

func validPath(value string) bool {
	if value == "" || len(value) > 256 || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "?#\\\r\n\t") {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.IsAbs() == false && parsed.Host == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func nonNegative(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}
