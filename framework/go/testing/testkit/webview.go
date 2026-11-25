package testkit

import "sync"

type WebViewEvent struct {
	Name    string
	Payload string
}

type FakeWebViewBridge struct {
	mu      sync.Mutex
	events  []WebViewEvent
	scripts []string
	results map[string]string
}

func NewFakeWebViewBridge() *FakeWebViewBridge {
	return &FakeWebViewBridge{results: make(map[string]string)}
}

func (bridge *FakeWebViewBridge) Emit(name string, payload string) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.events = append(bridge.events, WebViewEvent{Name: name, Payload: payload})
}

func (bridge *FakeWebViewBridge) SetEvalResult(script string, result string) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.results[script] = result
}

func (bridge *FakeWebViewBridge) Eval(script string) string {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.scripts = append(bridge.scripts, script)
	return bridge.results[script]
}

func (bridge *FakeWebViewBridge) Events() []WebViewEvent {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	return append([]WebViewEvent(nil), bridge.events...)
}

func (bridge *FakeWebViewBridge) Scripts() []string {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	return append([]string(nil), bridge.scripts...)
}
