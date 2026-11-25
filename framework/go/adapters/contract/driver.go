package contract

import "context"

type ProbeRequest struct {
	Meta   RequestMeta `json:"meta"`
	Filter string      `json:"filter,omitempty"`
}

type DeviceSet struct {
	Devices []Device `json:"devices"`
}

type Device struct {
	ID          modelID `json:"id"`
	DisplayName string  `json:"display_name"`
	Kind        string  `json:"kind"`
}

type DeviceAuthorizationRequest struct {
	Meta     RequestMeta `json:"meta"`
	DeviceID modelID     `json:"device_id"`
	Actions  []string    `json:"actions"`
}

type DeviceGrant struct {
	ID       GrantID  `json:"id"`
	DeviceID modelID  `json:"device_id"`
	Actions  []string `json:"actions"`
}

type DriverOperation struct {
	Meta       RequestMeta `json:"meta"`
	GrantID    GrantID     `json:"grant_id"`
	Operation  string      `json:"operation"`
	Parameters []NameValue `json:"parameters,omitempty"`
}

type DeviceObserveRequest struct {
	Meta     RequestMeta `json:"meta"`
	DeviceID modelID     `json:"device_id"`
	Cursor   string      `json:"cursor,omitempty"`
}

type DeviceEvent struct {
	Sequence uint64  `json:"sequence"`
	DeviceID modelID `json:"device_id"`
	Type     string  `json:"type"`
	Terminal bool    `json:"terminal"`
}

type DeviceEventStream interface {
	Next(context.Context) (DeviceEvent, error)
	Close(context.Context) error
}
