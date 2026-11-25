package remoteservice

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/resilience"
)

type ClientOptions struct {
	HTTPClient   *http.Client
	Policy       Policy
	Retry        RetryPolicy
	Limiter      *resilience.Limiter
	Breaker      *resilience.Breaker
	Clock        frameworkclock.Clock
	UserAgent    string
	MaxBodyBytes int64
	TLSRoot      *x509.CertPool
	TLSPins      TLSPinSet
}

type Client struct {
	httpClient *http.Client
	policy     Policy
	retry      RetryPolicy
	limiter    *resilience.Limiter
	breaker    *resilience.Breaker
	clock      frameworkclock.Clock
	userAgent  string
}

func NewClient(options ClientOptions) (*Client, error) {
	policy := options.Policy
	if policy.AllowedSchemes == nil && policy.AllowedMethods == nil && policy.AllowedHeaders == nil {
		policy = DefaultHTTPPolicy()
	}
	if options.MaxBodyBytes > 0 {
		policy.MaxBodyBytes = options.MaxBodyBytes
	}
	if policy.MaxBodyBytes < 1 {
		policy.MaxBodyBytes = defaultMaxBodyBytes
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Transport: clientTransport(options),
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	retry := options.Retry
	if retry.MaxAttempts < 1 {
		retry.MaxAttempts = 1
	}
	if retry.MaxAttempts > 1 && retry.BaseDelay <= 0 {
		retry.BaseDelay = 100 * time.Millisecond
	}
	if retry.MaxDelay <= 0 {
		retry.MaxDelay = 5 * time.Second
	}
	if options.Clock == nil {
		options.Clock = frameworkclock.System{}
	}
	return &Client{
		httpClient: httpClient, policy: policy, retry: retry,
		limiter: options.Limiter, breaker: options.Breaker, clock: options.Clock,
		userAgent: options.UserAgent,
	}, nil
}

func clientTransport(options ClientOptions) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if options.TLSRoot != nil || !options.TLSPins.Empty() {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: options.TLSRoot}
		if !options.TLSPins.Empty() {
			tlsConfig.VerifyConnection = func(state tls.ConnectionState) error {
				return options.TLSPins.VerifyPeerCertificates(nil, state.VerifiedChains)
			}
		}
		transport.TLSClientConfig = tlsConfig
	}
	return transport
}

func (client *Client) Do(ctx context.Context, request Request) (*Response, error) {
	if client == nil || ctx == nil {
		return nil, ErrInvalidRequest
	}
	parsed, err := client.policy.Validate(request.Method, request.URL, request.Headers, int64(len(request.Body)))
	if err != nil {
		return nil, err
	}
	ctx, span, err := startRemoteSpan(ctx, request)
	if err != nil {
		return nil, err
	}
	ctx = withRemoteSpan(ctx, span)
	if client.limiter != nil {
		if err := client.limiter.Wait(ctx, 1); err != nil {
			return nil, err
		}
	}
	execute := func(callContext context.Context) error {
		response, callErr := client.doWithRetry(callContext, parsed.String(), request)
		if callErr != nil {
			return callErr
		}
		*responseHolder(callContext) = response
		return nil
	}
	holderContext := context.WithValue(ctx, responseContextKey{}, &Response{})
	if client.breaker != nil {
		err = client.breaker.Execute(holderContext, execute)
	} else {
		err = execute(holderContext)
	}
	if err != nil {
		return nil, err
	}
	value := *responseHolder(holderContext)
	value.TraceID = span.Trace.TraceID
	return &value, nil
}

func (client *Client) doWithRetry(ctx context.Context, rawURL string, request Request) (Response, error) {
	var lastErr error
	for attempt := 1; attempt <= client.retry.MaxAttempts; attempt++ {
		response, err := client.doOnce(ctx, rawURL, request, attempt)
		if err == nil && !retryableStatus(response.StatusCode) {
			return response, nil
		}
		if err == nil {
			lastErr = fmt.Errorf("%w: status=%d", ErrRetryExhausted, response.StatusCode)
		} else {
			lastErr = err
		}
		if attempt == client.retry.MaxAttempts || !request.isReplaySafe() {
			if err == nil {
				return response, lastErr
			}
			return Response{Attempts: attempt}, lastErr
		}
		delay := client.retryDelay(attempt)
		select {
		case <-ctx.Done():
			return Response{Attempts: attempt}, ctx.Err()
		case <-client.clock.After(delay):
		}
	}
	return Response{Attempts: client.retry.MaxAttempts}, lastErr
}

func (client *Client) doOnce(ctx context.Context, rawURL string, request Request, attempt int) (Response, error) {
	callContext := ctx
	var cancel context.CancelFunc
	if request.Timeout > 0 {
		callContext, cancel = context.WithTimeout(ctx, request.Timeout)
		defer cancel()
	}
	httpRequest, err := http.NewRequestWithContext(callContext, request.Method, rawURL, bytes.NewReader(request.Body))
	if err != nil {
		return Response{Attempts: attempt}, err
	}
	for name, values := range request.Headers {
		for _, value := range values {
			httpRequest.Header.Add(name, value)
		}
	}
	if request.RequestID != "" {
		httpRequest.Header.Set("X-Request-ID", request.RequestID)
	}
	if request.IdempotencyKey != "" {
		httpRequest.Header.Set("Idempotency-Key", request.IdempotencyKey)
	}
	if client.userAgent != "" && httpRequest.Header.Get("User-Agent") == "" {
		httpRequest.Header.Set("User-Agent", client.userAgent)
	}
	if span, ok := remoteSpanFrom(ctx); ok {
		for name, value := range span.OutgoingHeaders {
			if value != "" {
				httpRequest.Header.Set(name, value)
			}
		}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return Response{Attempts: attempt}, err
	}
	defer httpResponse.Body.Close()
	limit := client.policy.MaxBodyBytes + 1
	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, limit))
	if err != nil {
		return Response{Attempts: attempt}, err
	}
	if int64(len(body)) > client.policy.MaxBodyBytes {
		return Response{Attempts: attempt}, ErrBodyTooLarge
	}
	return Response{StatusCode: httpResponse.StatusCode, Headers: httpResponse.Header.Clone(), Body: body, Attempts: attempt}, nil
}

func (client *Client) retryDelay(attempt int) time.Duration {
	delay := client.retry.BaseDelay
	for index := 1; index < attempt; index++ {
		delay *= 2
		if delay >= client.retry.MaxDelay {
			return client.retry.MaxDelay
		}
	}
	if delay > client.retry.MaxDelay {
		return client.retry.MaxDelay
	}
	return delay
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}

func (request Request) isReplaySafe() bool {
	switch request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return request.IdempotencyKey != ""
	}
}

type responseContextKey struct{}

func responseHolder(ctx context.Context) *Response {
	holder, ok := ctx.Value(responseContextKey{}).(*Response)
	if !ok || holder == nil {
		panic(errors.New("RemoteService response holder missing"))
	}
	return holder
}
