package systemstatus

import (
	"context"
	"net"
	"os"
)

type UnknownPowerSource struct{}

func (UnknownPowerSource) Snapshot(ctx context.Context) (PowerSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return PowerSnapshot{}, err
	}
	return PowerSnapshot{State: PowerStateUnknown}, nil
}

type NetworkInterfacesSource struct {
	Interfaces func() ([]net.Interface, error)
}

func (source NetworkInterfacesSource) Snapshot(ctx context.Context) (NetworkSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return NetworkSnapshot{}, err
	}
	interfaces := source.Interfaces
	if interfaces == nil {
		interfaces = net.Interfaces
	}
	values, err := interfaces()
	if err != nil {
		return NetworkSnapshot{}, err
	}
	snapshot := NetworkSnapshot{Interfaces: make([]NetworkInterface, 0, len(values))}
	for _, value := range values {
		networkInterface := NetworkInterface{
			Name:     value.Name,
			Up:       value.Flags&net.FlagUp != 0,
			Loopback: value.Flags&net.FlagLoopback != 0,
		}
		if networkInterface.Up && !networkInterface.Loopback {
			snapshot.Online = true
		}
		snapshot.Interfaces = append(snapshot.Interfaces, networkInterface)
	}
	return snapshot, nil
}

type UnknownScreenSource struct{}

func (UnknownScreenSource) Snapshot(ctx context.Context) ([]ScreenSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []ScreenSnapshot{}, nil
}

type UnknownAudioSource struct{}

func (UnknownAudioSource) Snapshot(ctx context.Context) (AudioSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return AudioSnapshot{}, err
	}
	return AudioSnapshot{}, nil
}

type EnvironmentProxySource struct{}

func (EnvironmentProxySource) Snapshot(ctx context.Context) (ProxySnapshot, error) {
	if err := ctx.Err(); err != nil {
		return ProxySnapshot{}, err
	}
	return ProxySnapshot{
		HTTP:    firstEnv("HTTP_PROXY", "http_proxy"),
		HTTPS:   firstEnv("HTTPS_PROXY", "https_proxy"),
		NoProxy: firstEnv("NO_PROXY", "no_proxy"),
	}, nil
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
