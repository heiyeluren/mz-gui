package systemstatus

import "time"

type PowerState string

const (
	PowerStateUnknown     PowerState = "unknown"
	PowerStateCharging    PowerState = "charging"
	PowerStateDischarging PowerState = "discharging"
	PowerStateFull        PowerState = "full"
)

type Snapshot struct {
	ObservedAt time.Time        `json:"observed_at"`
	Power      PowerSnapshot    `json:"power"`
	Network    NetworkSnapshot  `json:"network"`
	Screens    []ScreenSnapshot `json:"screens"`
	Audio      AudioSnapshot    `json:"audio"`
	Proxy      ProxySnapshot    `json:"proxy"`
}

type PowerSnapshot struct {
	State          PowerState `json:"state"`
	BatteryPercent *int       `json:"battery_percent,omitempty"`
}

type NetworkSnapshot struct {
	Online     bool               `json:"online"`
	Interfaces []NetworkInterface `json:"interfaces"`
}

type NetworkInterface struct {
	Name     string `json:"name"`
	Up       bool   `json:"up"`
	Loopback bool   `json:"loopback"`
}

type ScreenSnapshot struct {
	ID     string  `json:"id"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Scale  float64 `json:"scale"`
}

type AudioSnapshot struct {
	Inputs  []AudioDevice `json:"inputs"`
	Outputs []AudioDevice `json:"outputs"`
}

type AudioDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProxySnapshot struct {
	HTTP    string `json:"http,omitempty"`
	HTTPS   string `json:"https,omitempty"`
	NoProxy string `json:"no_proxy,omitempty"`
}

func IntPtr(value int) *int {
	return &value
}
