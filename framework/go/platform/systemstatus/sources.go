package systemstatus

import (
	"context"
	"time"
)

type Clock interface {
	Now() time.Time
}

type PowerSource interface {
	Snapshot(context.Context) (PowerSnapshot, error)
}

type NetworkSource interface {
	Snapshot(context.Context) (NetworkSnapshot, error)
}

type ScreenSource interface {
	Snapshot(context.Context) ([]ScreenSnapshot, error)
}

type AudioSource interface {
	Snapshot(context.Context) (AudioSnapshot, error)
}

type ProxySource interface {
	Snapshot(context.Context) (ProxySnapshot, error)
}

type PowerSourceFunc func(context.Context) (PowerSnapshot, error)

func (function PowerSourceFunc) Snapshot(ctx context.Context) (PowerSnapshot, error) {
	return function(ctx)
}

type NetworkSourceFunc func(context.Context) (NetworkSnapshot, error)

func (function NetworkSourceFunc) Snapshot(ctx context.Context) (NetworkSnapshot, error) {
	return function(ctx)
}

type ScreenSourceFunc func(context.Context) ([]ScreenSnapshot, error)

func (function ScreenSourceFunc) Snapshot(ctx context.Context) ([]ScreenSnapshot, error) {
	return function(ctx)
}

type AudioSourceFunc func(context.Context) (AudioSnapshot, error)

func (function AudioSourceFunc) Snapshot(ctx context.Context) (AudioSnapshot, error) {
	return function(ctx)
}

type ProxySourceFunc func(context.Context) (ProxySnapshot, error)

func (function ProxySourceFunc) Snapshot(ctx context.Context) (ProxySnapshot, error) {
	return function(ctx)
}
