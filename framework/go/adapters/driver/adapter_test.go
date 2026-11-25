package driver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/contract"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func TestMemoryDriverProbeAuthorizeInvokeAndObserve(t *testing.T) {
	adapter, err := New(Options{Devices: []Device{{
		ID: "loopback.speaker", DisplayName: "Loopback Speaker", Kind: "audio",
		Actions: []string{"play", "stop"}, Operations: []string{"beep"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Close(context.Background()) })

	set, err := adapter.Probe(context.Background(), contract.ProbeRequest{Meta: readMeta(t, "probe-1"), Filter: "audio"})
	if err != nil || len(set.Devices) != 1 || set.Devices[0].ID != "loopback.speaker" {
		t.Fatalf("Probe 错误: %#v err=%v", set, err)
	}
	grant, err := adapter.Authorize(context.Background(), contract.DeviceAuthorizationRequest{
		Meta: readMeta(t, "auth-1"), DeviceID: "loopback.speaker", Actions: []string{"play"},
	})
	if err != nil || grant.ID == "" {
		t.Fatal(err)
	}
	task, err := adapter.Invoke(context.Background(), contract.DriverOperation{
		Meta: writeMeta(t, "inv-1"), GrantID: grant.ID, Operation: "beep",
	})
	if err != nil || task.Type != "driver.beep" {
		t.Fatalf("Invoke 错误: %#v err=%v", task, err)
	}
	stream, err := adapter.Observe(context.Background(), contract.DeviceObserveRequest{
		Meta: readMeta(t, "obs-1"), DeviceID: "loopback.speaker",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close(context.Background()) })
	event := mustNext(t, stream)
	if event.Type != "authorized" && event.Type != "invoked" {
		t.Fatalf("观察事件错误: %#v", event)
	}
}

func TestMemoryDriverRejectsUnauthorizedAndUnknownDevices(t *testing.T) {
	adapter, err := New(Options{Devices: []Device{{
		ID: "loopback.mic", DisplayName: "Loopback Mic", Kind: "audio",
		Actions: []string{"record"}, Operations: []string{"capture"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Close(context.Background()) })
	if _, err := adapter.Authorize(context.Background(), contract.DeviceAuthorizationRequest{
		Meta: readMeta(t, "auth-2"), DeviceID: "missing.dev", Actions: []string{"record"},
	}); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("未知设备错误 = %v", err)
	}
	if _, err := adapter.Invoke(context.Background(), contract.DriverOperation{
		Meta: writeMeta(t, "inv-2"), GrantID: "grant-missing", Operation: "capture",
	}); !errors.Is(err, ErrDenied) {
		t.Fatalf("无授权 Invoke 错误 = %v", err)
	}
}

func mustNext(t *testing.T, stream contract.DeviceEventStream) contract.DeviceEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := stream.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func writeMeta(t *testing.T, requestID string) contract.RequestMeta {
	t.Helper()
	meta := readMeta(t, requestID)
	meta.IdempotencyKey = model.ID(requestID)
	return meta
}

func readMeta(t *testing.T, requestID string) contract.RequestMeta {
	t.Helper()
	deadline, err := model.NewTimestamp(time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	version, err := model.ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	return contract.RequestMeta{
		RequestID: model.ID(requestID), TraceID: "trace-1", Deadline: deadline,
		Caller: model.Caller{Component: "test.driver", Version: version, InstanceID: "inst-1"},
	}
}
