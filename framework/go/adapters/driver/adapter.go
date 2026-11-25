// Package driver 提供产品无关的内存 DriverAdapter。
package driver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/contract"
	"github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

var (
	ErrUnknownDevice = errors.New("设备不存在")
	ErrDenied        = errors.New("驱动操作未获授权")
	ErrClosed        = errors.New("DriverAdapter 已关闭")
)

type Device struct {
	ID          string
	DisplayName string
	Kind        string
	Actions     []string
	Operations  []string
}

type Options struct {
	Devices []Device
	Info    contract.AdapterInfo
}

type grant struct {
	id       contract.GrantID
	deviceID model.ID
	actions  []string
}

type Adapter struct {
	info    contract.AdapterInfo
	devices []Device

	mu       sync.Mutex
	closed   bool
	changed  chan struct{}
	nextID   uint64
	grants   map[string]grant
	events   []contract.DeviceEvent
}

var _ contract.DriverAdapter = (*Adapter)(nil)

func New(options Options) (*Adapter, error) {
	if len(options.Devices) == 0 {
		return nil, contract.ErrInvalidContract
	}
	seen := make(map[string]struct{}, len(options.Devices))
	cloned := make([]Device, 0, len(options.Devices))
	for _, device := range options.Devices {
		if _, err := model.ParseID(device.ID); err != nil || strings.TrimSpace(device.DisplayName) == "" ||
			strings.TrimSpace(device.Kind) == "" || len(device.Actions) == 0 || len(device.Operations) == 0 {
			return nil, contract.ErrInvalidContract
		}
		if _, exists := seen[device.ID]; exists {
			return nil, contract.ErrInvalidContract
		}
		seen[device.ID] = struct{}{}
		cloned = append(cloned, Device{
			ID: device.ID, DisplayName: device.DisplayName, Kind: device.Kind,
			Actions: append([]string(nil), device.Actions...), Operations: append([]string(nil), device.Operations...),
		})
	}
	info := options.Info
	if info.ID == "" {
		version, err := model.ParseVersion("1.0.0")
		if err != nil {
			return nil, err
		}
		info = contract.AdapterInfo{
			ID: "adapter.driver.memory", DisplayName: "Memory Driver",
			BuildID: "memory-1", Protocol: version,
			Capabilities: []contract.Capability{{ID: "adapter.contract", Stability: contract.StabilityExperimental}},
		}
	}
	if err := info.Validate(); err != nil {
		return nil, err
	}
	return &Adapter{
		info: info, devices: cloned, changed: make(chan struct{}),
		grants: make(map[string]grant),
	}, nil
}

func (adapter *Adapter) Info(context.Context) (contract.AdapterInfo, error) {
	if adapter == nil {
		return contract.AdapterInfo{}, contract.ErrInvalidContract
	}
	return adapter.info, nil
}

func (adapter *Adapter) Health(context.Context) (health.Report, error) {
	if adapter == nil {
		return health.Report{}, contract.ErrInvalidContract
	}
	adapter.mu.Lock()
	closed := adapter.closed
	adapter.mu.Unlock()
	observed, err := model.NewTimestamp(time.Now().UTC())
	if err != nil {
		return health.Report{}, err
	}
	status := health.StatusHealthy
	if closed {
		status = health.StatusUnhealthy
	}
	return health.Report{Status: status, ObservedAt: observed, Build: adapter.info.Protocol}, nil
}

func (adapter *Adapter) Close(context.Context) error {
	if adapter == nil {
		return contract.ErrInvalidContract
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return nil
	}
	adapter.closed = true
	adapter.notifyLocked()
	return nil
}

func (adapter *Adapter) Probe(ctx context.Context, request contract.ProbeRequest) (contract.DeviceSet, error) {
	if err := adapter.guard(ctx, request.Meta, false); err != nil {
		return contract.DeviceSet{}, err
	}
	filter := strings.ToLower(strings.TrimSpace(request.Filter))
	devices := make([]contract.Device, 0)
	for _, device := range adapter.devices {
		if filter != "" && !strings.Contains(strings.ToLower(device.ID), filter) && !strings.Contains(strings.ToLower(device.Kind), filter) &&
			!strings.Contains(strings.ToLower(device.DisplayName), filter) {
			continue
		}
		devices = append(devices, contract.Device{ID: model.ID(device.ID), DisplayName: device.DisplayName, Kind: device.Kind})
	}
	return contract.DeviceSet{Devices: devices}, nil
}

func (adapter *Adapter) Authorize(ctx context.Context, request contract.DeviceAuthorizationRequest) (contract.DeviceGrant, error) {
	if err := adapter.guard(ctx, request.Meta, false); err != nil {
		return contract.DeviceGrant{}, err
	}
	device, ok := adapter.lookup(string(request.DeviceID))
	if !ok {
		return contract.DeviceGrant{}, ErrUnknownDevice
	}
	if len(request.Actions) == 0 || !subset(request.Actions, device.Actions) {
		return contract.DeviceGrant{}, ErrDenied
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return contract.DeviceGrant{}, ErrClosed
	}
	adapter.nextID++
	id := contract.GrantID(fmt.Sprintf("grant-%d", adapter.nextID))
	adapter.grants[string(id)] = grant{id: id, deviceID: request.DeviceID, actions: append([]string(nil), request.Actions...)}
	adapter.appendEventLocked(request.DeviceID, "authorized", false)
	return contract.DeviceGrant{ID: id, DeviceID: request.DeviceID, Actions: append([]string(nil), request.Actions...)}, nil
}

func (adapter *Adapter) Invoke(ctx context.Context, operation contract.DriverOperation) (contract.TaskRef, error) {
	if err := adapter.guard(ctx, operation.Meta, true); err != nil {
		return contract.TaskRef{}, err
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return contract.TaskRef{}, ErrClosed
	}
	item, exists := adapter.grants[string(operation.GrantID)]
	if !exists {
		return contract.TaskRef{}, ErrDenied
	}
	device, ok := adapter.lookup(string(item.deviceID))
	if !ok || !contains(device.Operations, operation.Operation) {
		return contract.TaskRef{}, ErrDenied
	}
	adapter.nextID++
	taskID := model.ID(fmt.Sprintf("task-%d", adapter.nextID))
	adapter.appendEventLocked(item.deviceID, "invoked", false)
	return contract.TaskRef{ID: taskID, Type: "driver." + operation.Operation}, nil
}

func (adapter *Adapter) Observe(ctx context.Context, request contract.DeviceObserveRequest) (contract.DeviceEventStream, error) {
	if err := adapter.guard(ctx, request.Meta, false); err != nil {
		return nil, err
	}
	if _, ok := adapter.lookup(string(request.DeviceID)); !ok {
		return nil, ErrUnknownDevice
	}
	cursor := uint64(0)
	if strings.TrimSpace(request.Cursor) != "" {
		var parsed uint64
		if _, err := fmt.Sscanf(request.Cursor, "%d", &parsed); err != nil {
			return nil, contract.ErrInvalidContract
		}
		cursor = parsed
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return nil, ErrClosed
	}
	return &eventStream{adapter: adapter, deviceID: request.DeviceID, cursor: cursor}, nil
}

type eventStream struct {
	adapter  *Adapter
	deviceID model.ID
	cursor   uint64
	closed   bool
}

func (stream *eventStream) Next(ctx context.Context) (contract.DeviceEvent, error) {
	if stream == nil || stream.adapter == nil || ctx == nil {
		return contract.DeviceEvent{}, contract.ErrInvalidContract
	}
	for {
		stream.adapter.mu.Lock()
		if stream.closed || stream.adapter.closed {
			stream.adapter.mu.Unlock()
			return contract.DeviceEvent{}, ErrClosed
		}
		for _, event := range stream.adapter.events {
			if event.DeviceID == stream.deviceID && event.Sequence > stream.cursor {
				stream.cursor = event.Sequence
				stream.adapter.mu.Unlock()
				return event, nil
			}
		}
		wait := stream.adapter.changed
		stream.adapter.mu.Unlock()
		select {
		case <-ctx.Done():
			return contract.DeviceEvent{}, ctx.Err()
		case <-wait:
		}
	}
}

func (stream *eventStream) Close(context.Context) error {
	if stream == nil {
		return contract.ErrInvalidContract
	}
	stream.adapter.mu.Lock()
	defer stream.adapter.mu.Unlock()
	stream.closed = true
	return nil
}

func (adapter *Adapter) lookup(id string) (Device, bool) {
	for _, device := range adapter.devices {
		if device.ID == id {
			return device, true
		}
	}
	return Device{}, false
}

func (adapter *Adapter) appendEventLocked(deviceID model.ID, eventType string, terminal bool) {
	adapter.events = append(adapter.events, contract.DeviceEvent{
		Sequence: uint64(len(adapter.events) + 1), DeviceID: deviceID, Type: eventType, Terminal: terminal,
	})
	adapter.notifyLocked()
}

func (adapter *Adapter) notifyLocked() {
	close(adapter.changed)
	adapter.changed = make(chan struct{})
}

func (adapter *Adapter) guard(ctx context.Context, meta contract.RequestMeta, write bool) error {
	if adapter == nil || ctx == nil {
		return contract.ErrInvalidContract
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := meta.Validate(time.Now().UTC(), write); err != nil {
		return contract.ErrInvalidContract
	}
	return nil
}

func subset(want, allow []string) bool {
	for _, item := range want {
		if !contains(allow, item) {
			return false
		}
	}
	return true
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
