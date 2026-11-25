package remoteservice

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

type ProxyDiagnosticStatus string

const (
	ProxyStatusUnknown      ProxyDiagnosticStatus = "unknown"
	ProxyStatusAvailable    ProxyDiagnosticStatus = "available"
	ProxyStatusAuthRequired ProxyDiagnosticStatus = "proxy_auth_required"
	ProxyStatusUnavailable  ProxyDiagnosticStatus = "proxy_unavailable"
)

type ProxyDiagnosticOptions struct {
	HTTPClient *http.Client
	TargetURL  string
	ProxyURL   string
	Timeout    time.Duration
	Clock      frameworkclock.Clock
}

type ProxyDiagnosticResult struct {
	Status                 ProxyDiagnosticStatus
	ProxyURL               string
	TargetHost             string
	CheckedAt              time.Time
	Latency                time.Duration
	StatusCode             int
	RequiresAuthentication bool
	AuthScheme             string
	Reason                 string
	Error                  string
}

type ProxyDiagnosticProbe struct {
	client     *http.Client
	targetURL  string
	proxyURL   string
	targetHost string
	timeout    time.Duration
	clock      frameworkclock.Clock
}

func NewProxyDiagnosticProbe(options ProxyDiagnosticOptions) (*ProxyDiagnosticProbe, error) {
	target, err := parseProxyDiagnosticURL(options.TargetURL)
	if err != nil {
		return nil, err
	}
	proxy, err := parseProxyDiagnosticURL(options.ProxyURL)
	if err != nil {
		return nil, err
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	if options.Clock == nil {
		options.Clock = frameworkclock.System{}
	}
	client := options.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(proxy)
		client = &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &ProxyDiagnosticProbe{
		client: client, targetURL: target.String(), proxyURL: proxy.String(),
		targetHost: target.Hostname(), timeout: options.Timeout, clock: options.Clock,
	}, nil
}

func (probe *ProxyDiagnosticProbe) Check(ctx context.Context) (ProxyDiagnosticResult, error) {
	if probe == nil || probe.client == nil || ctx == nil {
		return ProxyDiagnosticResult{}, ErrInvalidProxyDiagnostic
	}
	start := time.Now()
	callContext, cancel := context.WithTimeout(ctx, probe.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callContext, http.MethodGet, probe.targetURL, nil)
	if err != nil {
		return ProxyDiagnosticResult{}, err
	}
	response, err := probe.client.Do(request)
	if err != nil {
		return probe.result(ProxyStatusUnavailable, 0, start, false, "", "remote.proxy_unavailable", err.Error()), nil
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusProxyAuthRequired {
		return probe.result(
			ProxyStatusAuthRequired, response.StatusCode, start, true,
			proxyAuthScheme(response.Header.Get("Proxy-Authenticate")), "remote.proxy_auth_required", "",
		), nil
	}
	if response.StatusCode >= 200 && response.StatusCode < 400 {
		return probe.result(ProxyStatusAvailable, response.StatusCode, start, false, "", "", ""), nil
	}
	return probe.result(ProxyStatusUnavailable, response.StatusCode, start, false, "", "remote.proxy_unavailable", ""), nil
}

func (probe *ProxyDiagnosticProbe) result(status ProxyDiagnosticStatus, statusCode int, start time.Time, requiresAuthentication bool, authScheme, reason, errorText string) ProxyDiagnosticResult {
	latency := time.Since(start)
	if latency <= 0 {
		latency = time.Nanosecond
	}
	return ProxyDiagnosticResult{
		Status: status, ProxyURL: probe.proxyURL, TargetHost: probe.targetHost, CheckedAt: probe.clock.Now(),
		Latency: latency, StatusCode: statusCode, RequiresAuthentication: requiresAuthentication,
		AuthScheme: authScheme, Reason: reason, Error: errorText,
	}
}

func parseProxyDiagnosticURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil {
		return nil, ErrInvalidProxyDiagnostic
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrInvalidProxyDiagnostic
	}
	return parsed, nil
}

func proxyAuthScheme(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	if index := strings.IndexAny(header, " \t,"); index >= 0 {
		return header[:index]
	}
	return header
}
