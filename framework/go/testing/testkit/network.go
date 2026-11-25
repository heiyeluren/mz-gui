package testkit

import (
	"bytes"
	"io"
	"net/http"
	"sync"
)

type HTTPFixture struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	Err        error
}

type RecordedRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

type FakeRoundTripper struct {
	mu       sync.Mutex
	fixtures []HTTPFixture
	requests []RecordedRequest
}

func NewFakeRoundTripper(fixtures ...HTTPFixture) *FakeRoundTripper {
	return &FakeRoundTripper{fixtures: append([]HTTPFixture(nil), fixtures...)}
}

func (transport *FakeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		body, _ := io.ReadAll(request.Body)
		_ = request.Body.Close()
		return transport.roundTripWithBody(request, body)
	}
	return transport.roundTripWithBody(request, nil)
}

func (transport *FakeRoundTripper) roundTripWithBody(request *http.Request, body []byte) (*http.Response, error) {
	transport.mu.Lock()
	transport.requests = append(transport.requests, RecordedRequest{
		Method: request.Method, URL: request.URL.String(), Header: request.Header.Clone(), Body: body,
	})
	fixture := HTTPFixture{StatusCode: http.StatusOK}
	if len(transport.fixtures) > 0 {
		fixture = transport.fixtures[0]
		transport.fixtures = transport.fixtures[1:]
	}
	transport.mu.Unlock()
	if fixture.Err != nil {
		return nil, fixture.Err
	}
	if fixture.StatusCode == 0 {
		fixture.StatusCode = http.StatusOK
	}
	return &http.Response{
		StatusCode: fixture.StatusCode,
		Header:     fixture.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(fixture.Body)),
		Request:    request,
	}, nil
}

func (transport *FakeRoundTripper) Requests() []RecordedRequest {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]RecordedRequest(nil), transport.requests...)
}
