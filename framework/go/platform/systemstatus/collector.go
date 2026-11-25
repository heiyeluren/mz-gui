package systemstatus

import (
	"context"
	"fmt"
	"time"
)

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

type CollectorOptions struct {
	Clock   Clock
	Power   PowerSource
	Network NetworkSource
	Screens ScreenSource
	Audio   AudioSource
	Proxy   ProxySource
}

type Collector struct {
	clock   Clock
	power   PowerSource
	network NetworkSource
	screens ScreenSource
	audio   AudioSource
	proxy   ProxySource
}

func NewCollector(options CollectorOptions) *Collector {
	if options.Clock == nil {
		options.Clock = realClock{}
	}
	if options.Power == nil {
		options.Power = UnknownPowerSource{}
	}
	if options.Network == nil {
		options.Network = NetworkInterfacesSource{}
	}
	if options.Screens == nil {
		options.Screens = UnknownScreenSource{}
	}
	if options.Audio == nil {
		options.Audio = UnknownAudioSource{}
	}
	if options.Proxy == nil {
		options.Proxy = EnvironmentProxySource{}
	}
	return &Collector{
		clock: options.Clock, power: options.Power, network: options.Network,
		screens: options.Screens, audio: options.Audio, proxy: options.Proxy,
	}
}

func NewDefaultCollector() *Collector {
	return NewCollector(CollectorOptions{})
}

func (collector *Collector) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	power, err := collector.power.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("采集电源状态: %w", err)
	}
	network, err := collector.network.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("采集网络状态: %w", err)
	}
	screens, err := collector.screens.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("采集屏幕状态: %w", err)
	}
	audio, err := collector.audio.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("采集音频状态: %w", err)
	}
	proxy, err := collector.proxy.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("采集系统代理状态: %w", err)
	}
	return Snapshot{
		ObservedAt: collector.clock.Now(),
		Power:      power,
		Network:    network,
		Screens:    screens,
		Audio:      audio,
		Proxy:      proxy,
	}, nil
}
