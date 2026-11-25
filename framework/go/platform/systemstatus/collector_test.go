package systemstatus

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestCollectorBuildsUnifiedSnapshot(t *testing.T) {
	t.Parallel()
	observedAt := time.Date(2026, 8, 16, 2, 30, 0, 0, time.UTC)
	collector := NewCollector(CollectorOptions{
		Clock: fixedClock{now: observedAt},
		Power: PowerSourceFunc(func(context.Context) (PowerSnapshot, error) {
			return PowerSnapshot{State: PowerStateCharging, BatteryPercent: IntPtr(80)}, nil
		}),
		Network: NetworkSourceFunc(func(context.Context) (NetworkSnapshot, error) {
			return NetworkSnapshot{Online: true, Interfaces: []NetworkInterface{{Name: "en0", Up: true, Loopback: false}}}, nil
		}),
		Screens: ScreenSourceFunc(func(context.Context) ([]ScreenSnapshot, error) {
			return []ScreenSnapshot{{ID: "main", Width: 3024, Height: 1964, Scale: 2}}, nil
		}),
		Audio: AudioSourceFunc(func(context.Context) (AudioSnapshot, error) {
			return AudioSnapshot{Inputs: []AudioDevice{{ID: "mic", Name: "麦克风"}}, Outputs: []AudioDevice{{ID: "speaker", Name: "扬声器"}}}, nil
		}),
		Proxy: ProxySourceFunc(func(context.Context) (ProxySnapshot, error) {
			httpURL, _ := url.Parse("http://127.0.0.1:7890")
			return ProxySnapshot{HTTP: httpURL.String()}, nil
		}),
	})

	snapshot, err := collector.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("采集系统状态: %v", err)
	}
	if !snapshot.ObservedAt.Equal(observedAt) {
		t.Fatalf("采集时间 = %s", snapshot.ObservedAt)
	}
	if snapshot.Power.State != PowerStateCharging || snapshot.Power.BatteryPercent == nil || *snapshot.Power.BatteryPercent != 80 {
		t.Fatalf("电源状态异常: %#v", snapshot.Power)
	}
	if !snapshot.Network.Online || len(snapshot.Network.Interfaces) != 1 {
		t.Fatalf("网络状态异常: %#v", snapshot.Network)
	}
	if len(snapshot.Screens) != 1 || snapshot.Screens[0].Scale != 2 {
		t.Fatalf("屏幕状态异常: %#v", snapshot.Screens)
	}
	if len(snapshot.Audio.Inputs) != 1 || len(snapshot.Audio.Outputs) != 1 {
		t.Fatalf("音频状态异常: %#v", snapshot.Audio)
	}
	if snapshot.Proxy.HTTP != "http://127.0.0.1:7890" {
		t.Fatalf("代理状态异常: %#v", snapshot.Proxy)
	}
}

func TestCollectorPropagatesSourceError(t *testing.T) {
	t.Parallel()
	want := errors.New("network unavailable")
	collector := NewCollector(CollectorOptions{
		Network: NetworkSourceFunc(func(context.Context) (NetworkSnapshot, error) {
			return NetworkSnapshot{}, want
		}),
	})

	_, err := collector.Snapshot(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("错误 = %v，期望 %v", err, want)
	}
}

func TestDefaultNetworkSourceReportsUsableInterfaces(t *testing.T) {
	t.Parallel()
	source := NetworkInterfacesSource{Interfaces: func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "lo0", Flags: net.FlagUp | net.FlagLoopback},
			{Name: "en0", Flags: net.FlagUp | net.FlagBroadcast},
			{Name: "down0", Flags: 0},
		}, nil
	}}

	snapshot, err := source.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("采集网络接口: %v", err)
	}
	if !snapshot.Online || len(snapshot.Interfaces) != 3 {
		t.Fatalf("网络接口状态异常: %#v", snapshot)
	}
	if !snapshot.Interfaces[0].Loopback || !snapshot.Interfaces[1].Up || snapshot.Interfaces[2].Up {
		t.Fatalf("网络接口标记异常: %#v", snapshot.Interfaces)
	}
}

func TestEnvironmentProxySourceReadsProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:7890")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7891")
	t.Setenv("NO_PROXY", "localhost,127.0.0.1")

	snapshot, err := EnvironmentProxySource{}.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("采集代理环境: %v", err)
	}
	if snapshot.HTTP != "http://127.0.0.1:7890" || snapshot.HTTPS != "http://127.0.0.1:7891" || snapshot.NoProxy != "localhost,127.0.0.1" {
		t.Fatalf("代理环境异常: %#v", snapshot)
	}
}

func TestDefaultCollectorIsAvailable(t *testing.T) {
	snapshot, err := NewDefaultCollector().Snapshot(context.Background())
	if err != nil {
		t.Fatalf("默认系统状态采集失败: %v", err)
	}
	if snapshot.ObservedAt.IsZero() {
		t.Fatal("默认系统状态缺少采集时间")
	}
	if snapshot.Power.State == "" {
		t.Fatal("默认系统状态缺少电源状态")
	}
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}
