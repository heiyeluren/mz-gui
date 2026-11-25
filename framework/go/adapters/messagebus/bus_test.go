package messagebus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/contract"
	"github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func TestMemoryBusPublishSubscribeAckAndCursorResume(t *testing.T) {
	bus, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bus.Close(context.Background()) })

	first, err := bus.Publish(context.Background(), mustMessage(t, "runtime.events", "art-1", "idem-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := bus.Publish(context.Background(), mustMessage(t, "runtime.events", "art-2", "idem-2"))
	if err != nil {
		t.Fatal(err)
	}
	if first.MessageID == "" || first.MessageID == second.MessageID {
		t.Fatalf("消息 ID 异常: %#v %#v", first, second)
	}
	again, err := bus.Publish(context.Background(), mustMessage(t, "runtime.events", "art-1", "idem-1"))
	if err != nil || again.MessageID != first.MessageID {
		t.Fatalf("幂等发布失败: %#v err=%v", again, err)
	}

	stream, err := bus.Subscribe(context.Background(), contract.SubscriptionRequest{Meta: readMeta(t, "sub-1"), Topic: "runtime.events"})
	if err != nil {
		t.Fatal(err)
	}
	firstDelivery := mustNext(t, stream)
	if firstDelivery.Message.PayloadRef.ID != "art-1" || firstDelivery.Sequence != 1 {
		t.Fatalf("首条投递错误: %#v", firstDelivery)
	}
	if err := bus.Ack(context.Background(), firstDelivery.DeliveryID); err != nil {
		t.Fatal(err)
	}
	_ = stream.Close(context.Background())

	resumed, err := bus.Subscribe(context.Background(), contract.SubscriptionRequest{
		Meta: readMeta(t, "sub-2"), Topic: "runtime.events", Cursor: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resumed.Close(context.Background()) })
	secondDelivery := mustNext(t, resumed)
	if secondDelivery.Message.PayloadRef.ID != "art-2" || secondDelivery.Sequence != 2 {
		t.Fatalf("游标恢复错误: %#v", secondDelivery)
	}
}

func TestMemoryBusNackRedeliversAndRejectsInvalidTopics(t *testing.T) {
	bus, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bus.Close(context.Background()) })
	if _, err := bus.Publish(context.Background(), mustMessage(t, "Bad Topic", "art-1", "idem-1")); !errors.Is(err, contract.ErrInvalidContract) {
		t.Fatalf("非法 topic 错误 = %v", err)
	}
	if _, err := bus.Publish(context.Background(), mustMessage(t, "runtime.events", "art-1", "idem-1")); err != nil {
		t.Fatal(err)
	}
	stream, err := bus.Subscribe(context.Background(), contract.SubscriptionRequest{Meta: readMeta(t, "sub-3"), Topic: "runtime.events"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close(context.Background()) })
	delivery := mustNext(t, stream)
	if err := bus.Nack(context.Background(), contract.NackRequest{Meta: readMeta(t, "nack-1"), DeliveryID: delivery.DeliveryID}); err != nil {
		t.Fatal(err)
	}
	redelivered := mustNext(t, stream)
	if redelivered.Message.PayloadRef.ID != "art-1" || redelivered.DeliveryID == delivery.DeliveryID {
		t.Fatalf("Nack 重投错误: %#v", redelivered)
	}
}

func TestMemoryBusCloseStopsNextAndReportsHealth(t *testing.T) {
	bus, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := bus.Info(context.Background())
	if err != nil || info.Validate() != nil {
		t.Fatalf("Info 异常: %#v err=%v", info, err)
	}
	report, err := bus.Health(context.Background())
	if err != nil || report.Status != health.StatusHealthy {
		t.Fatalf("Health 异常: %#v err=%v", report, err)
	}
	stream, err := bus.Subscribe(context.Background(), contract.SubscriptionRequest{Meta: readMeta(t, "sub-4"), Topic: "runtime.events"})
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := stream.Next(ctx); !errors.Is(err, ErrClosed) && !errors.Is(err, context.DeadlineExceeded) {
		if err == nil || !strings.Contains(err.Error(), "关闭") && !errors.Is(err, ErrClosed) {
			t.Fatalf("关闭后 Next 错误 = %v", err)
		}
	}
	if _, err := bus.Publish(context.Background(), mustMessage(t, "runtime.events", "art-9", "idem-9")); !errors.Is(err, ErrClosed) {
		t.Fatalf("关闭后 Publish 错误 = %v", err)
	}
}

func mustNext(t *testing.T, stream contract.MessageStream) contract.DeliveredMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delivery, err := stream.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return delivery
}

func mustMessage(t *testing.T, topic, artifactID, idempotency string) contract.Message {
	t.Helper()
	return contract.Message{
		Meta: writeMeta(t, idempotency),
		Topic: topic,
		PayloadRef: contract.ArtifactRef{
			ID: model.ID(artifactID), SHA256: strings.Repeat("ab", 32), Bytes: 16,
		},
		ContentType: "application/json",
	}
}

func writeMeta(t *testing.T, idempotency string) contract.RequestMeta {
	t.Helper()
	meta := readMeta(t, idempotency)
	meta.IdempotencyKey = model.ID(idempotency)
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
		Caller: model.Caller{Component: "test.messagebus", Version: version, InstanceID: "inst-1"},
	}
}
