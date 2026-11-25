// Package messagebus 提供产品无关的进程内 MessageBus 适配器。
package messagebus

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/contract"
	"github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

var (
	ErrClosed         = errors.New("MessageBus 已关闭")
	ErrUnknownDelivery = errors.New("MessageBus 投递不存在")

	topicPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[./-][a-z0-9]+)*$`)
	contentTypePattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9][a-z0-9.+-]*$`)
	sha256Pattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type Options struct {
	Info        contract.AdapterInfo
	MaxInflight int
}

var _ contract.MessageBus = (*Bus)(nil)

type Bus struct {
	info        contract.AdapterInfo
	maxInflight int

	mu          sync.Mutex
	closed      bool
	changed     chan struct{}
	nextSeq     uint64
	nextDelivery uint64
	topics      map[string][]storedMessage
	idempotent  map[string]contract.PublishAck
	inflight    map[string]inflight
}

type storedMessage struct {
	sequence uint64
	id       model.ID
	message  contract.Message
}

type inflight struct {
	topic      string
	sequence   uint64
	message    contract.Message
	subscriber *stream
}

type stream struct {
	bus      *Bus
	topic    string
	cursor   uint64
	closed   bool
	pending  []uint64
}

func New(options Options) (*Bus, error) {
	info := options.Info
	if info.ID == "" {
		version, err := model.ParseVersion("1.0.0")
		if err != nil {
			return nil, err
		}
		info = contract.AdapterInfo{
			ID: "adapter.messagebus.memory", DisplayName: "Memory MessageBus",
			BuildID: "memory-1", Protocol: version,
			Capabilities: []contract.Capability{{ID: "adapter.contract", Stability: contract.StabilityExperimental}},
		}
	}
	if err := info.Validate(); err != nil {
		return nil, err
	}
	if options.MaxInflight < 0 {
		return nil, contract.ErrInvalidContract
	}
	if options.MaxInflight == 0 {
		options.MaxInflight = 128
	}
	return &Bus{
		info: info, maxInflight: options.MaxInflight, changed: make(chan struct{}),
		topics: make(map[string][]storedMessage), idempotent: make(map[string]contract.PublishAck),
		inflight: make(map[string]inflight),
	}, nil
}

func (bus *Bus) Info(context.Context) (contract.AdapterInfo, error) {
	if bus == nil {
		return contract.AdapterInfo{}, contract.ErrInvalidContract
	}
	return bus.info, nil
}

func (bus *Bus) Health(context.Context) (health.Report, error) {
	if bus == nil {
		return health.Report{}, contract.ErrInvalidContract
	}
	bus.mu.Lock()
	closed := bus.closed
	bus.mu.Unlock()
	observed, err := model.NewTimestamp(time.Now().UTC())
	if err != nil {
		return health.Report{}, err
	}
	status := health.StatusHealthy
	if closed {
		status = health.StatusUnhealthy
	}
	return health.Report{Status: status, ObservedAt: observed, Build: bus.info.Protocol}, nil
}

func (bus *Bus) Close(context.Context) error {
	if bus == nil {
		return contract.ErrInvalidContract
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if bus.closed {
		return nil
	}
	bus.closed = true
	bus.notifyLocked()
	return nil
}

func (bus *Bus) Publish(ctx context.Context, message contract.Message) (contract.PublishAck, error) {
	if err := bus.guard(ctx); err != nil {
		return contract.PublishAck{}, err
	}
	if err := message.Meta.Validate(time.Now().UTC(), true); err != nil || !validTopic(message.Topic) ||
		!contentTypePattern.MatchString(message.ContentType) || !validArtifact(message.PayloadRef) {
		return contract.PublishAck{}, contract.ErrInvalidContract
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if bus.closed {
		return contract.PublishAck{}, ErrClosed
	}
	key := string(message.Meta.IdempotencyKey)
	if ack, exists := bus.idempotent[key]; exists {
		return ack, nil
	}
	bus.nextSeq++
	seq := bus.nextSeq
	id := model.ID(fmt.Sprintf("msg-%d", seq))
	cloned := cloneMessage(message)
	bus.topics[message.Topic] = append(bus.topics[message.Topic], storedMessage{sequence: seq, id: id, message: cloned})
	ack := contract.PublishAck{MessageID: id}
	bus.idempotent[key] = ack
	bus.notifyLocked()
	return ack, nil
}

func (bus *Bus) Subscribe(ctx context.Context, request contract.SubscriptionRequest) (contract.MessageStream, error) {
	if err := bus.guard(ctx); err != nil {
		return nil, err
	}
	if err := request.Meta.Validate(time.Now().UTC(), false); err != nil || !validTopic(request.Topic) {
		return nil, contract.ErrInvalidContract
	}
	cursor := uint64(0)
	if strings.TrimSpace(request.Cursor) != "" {
		parsed, err := strconv.ParseUint(request.Cursor, 10, 64)
		if err != nil {
			return nil, contract.ErrInvalidContract
		}
		cursor = parsed
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if bus.closed {
		return nil, ErrClosed
	}
	return &stream{bus: bus, topic: request.Topic, cursor: cursor}, nil
}

func (bus *Bus) Ack(_ context.Context, deliveryID contract.DeliveryID) error {
	if bus == nil || deliveryID == "" {
		return contract.ErrInvalidContract
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if bus.closed {
		return ErrClosed
	}
	if _, exists := bus.inflight[string(deliveryID)]; !exists {
		return ErrUnknownDelivery
	}
	delete(bus.inflight, string(deliveryID))
	return nil
}

func (bus *Bus) Nack(_ context.Context, request contract.NackRequest) error {
	if bus == nil || request.DeliveryID == "" || request.RetryAfter < 0 {
		return contract.ErrInvalidContract
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if bus.closed {
		return ErrClosed
	}
	item, exists := bus.inflight[string(request.DeliveryID)]
	if !exists || item.subscriber == nil {
		return ErrUnknownDelivery
	}
	delete(bus.inflight, string(request.DeliveryID))
	item.subscriber.pending = append(item.subscriber.pending, item.sequence)
	bus.notifyLocked()
	return nil
}

func (stream *stream) Next(ctx context.Context) (contract.DeliveredMessage, error) {
	if stream == nil || stream.bus == nil {
		return contract.DeliveredMessage{}, contract.ErrInvalidContract
	}
	if ctx == nil {
		return contract.DeliveredMessage{}, context.Canceled
	}
	for {
		stream.bus.mu.Lock()
		if stream.closed || stream.bus.closed {
			stream.bus.mu.Unlock()
			return contract.DeliveredMessage{}, ErrClosed
		}
		if delivery, ok := stream.dequeueLocked(); ok {
			stream.bus.mu.Unlock()
			return delivery, nil
		}
		wait := stream.bus.changed
		stream.bus.mu.Unlock()
		select {
		case <-ctx.Done():
			return contract.DeliveredMessage{}, ctx.Err()
		case <-wait:
		}
	}
}

func (stream *stream) Close(context.Context) error {
	if stream == nil || stream.bus == nil {
		return contract.ErrInvalidContract
	}
	stream.bus.mu.Lock()
	defer stream.bus.mu.Unlock()
	stream.closed = true
	for id, item := range stream.bus.inflight {
		if item.subscriber == stream {
			delete(stream.bus.inflight, id)
		}
	}
	return nil
}

func (stream *stream) dequeueLocked() (contract.DeliveredMessage, bool) {
	if len(stream.pending) > 0 {
		seq := stream.pending[0]
		stream.pending = stream.pending[1:]
		return stream.deliverLocked(seq)
	}
	for _, item := range stream.bus.topics[stream.topic] {
		if item.sequence > stream.cursor {
			return stream.deliverLocked(item.sequence)
		}
	}
	return contract.DeliveredMessage{}, false
}

func (stream *stream) deliverLocked(sequence uint64) (contract.DeliveredMessage, bool) {
	if len(stream.bus.inflight) >= stream.bus.maxInflight {
		return contract.DeliveredMessage{}, false
	}
	var stored storedMessage
	found := false
	for _, item := range stream.bus.topics[stream.topic] {
		if item.sequence == sequence {
			stored = item
			found = true
			break
		}
	}
	if !found {
		return contract.DeliveredMessage{}, false
	}
	if sequence > stream.cursor {
		stream.cursor = sequence
	}
	stream.bus.nextDelivery++
	deliveryID := model.ID(fmt.Sprintf("del-%d", stream.bus.nextDelivery))
	stream.bus.inflight[string(deliveryID)] = inflight{
		topic: stream.topic, sequence: sequence, message: stored.message, subscriber: stream,
	}
	return contract.DeliveredMessage{
		DeliveryID: deliveryID, Sequence: sequence, Message: cloneMessage(stored.message),
	}, true
}

func (bus *Bus) notifyLocked() {
	close(bus.changed)
	bus.changed = make(chan struct{})
}

func (bus *Bus) guard(ctx context.Context) error {
	if bus == nil || ctx == nil {
		return contract.ErrInvalidContract
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func validTopic(topic string) bool {
	return topicPattern.MatchString(topic) && len(topic) <= 128
}

func validArtifact(ref contract.ArtifactRef) bool {
	if _, err := model.ParseID(ref.ID.String()); err != nil || ref.Bytes < 1 || !sha256Pattern.MatchString(ref.SHA256) {
		return false
	}
	return true
}

func cloneMessage(message contract.Message) contract.Message {
	cloned := message
	cloned.PayloadRef = contract.ArtifactRef{ID: message.PayloadRef.ID, SHA256: message.PayloadRef.SHA256, Bytes: message.PayloadRef.Bytes}
	return cloned
}
