// Package sidecargroup 提供产品无关的 Sidecar 多进程组合根。
package sidecargroup

import (
	"context"
	"errors"
	"reflect"

	"github.com/heiyeluren/mz-gui/framework/go/lifecycle"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/managedprocess"
)

var (
	ErrInvalidGroup = errors.New("Sidecar Group 参数无效")
	ErrNotReady     = errors.New("Sidecar 尚未就绪")
)

type ProcessManager interface {
	Start(context.Context, managedprocess.Spec) (managedprocess.Snapshot, error)
	Status() managedprocess.Snapshot
	Stop(context.Context) error
}

type Factory func(Declaration) ProcessManager

type Declaration struct {
	ID               string
	Process          managedprocess.Spec
	Requires         []string
	OptionalRequires []string
	FailurePolicy    lifecycle.FailurePolicy
	Timeouts         lifecycle.Timeouts
}

type Options struct {
	Lifecycle         lifecycle.Options
	Factory           Factory
	MaxParallelStarts int
}

type SidecarSnapshot struct {
	ID            string               `json:"id"`
	Required      bool                 `json:"required"`
	State         managedprocess.State `json:"state"`
	Running       bool                 `json:"running"`
	Ready         bool                 `json:"ready"`
	Degraded      bool                 `json:"degraded"`
	CrashLoop     bool                 `json:"crash_loop"`
	Incompatible  bool                 `json:"incompatible"`
	PID           int                  `json:"pid"`
	Version       string               `json:"version,omitempty"`
	ProtocolMajor uint32               `json:"protocol_major,omitempty"`
	ProtocolMinor uint32               `json:"protocol_minor,omitempty"`
	Capabilities  []string             `json:"capabilities,omitempty"`
}

type Snapshot struct {
	Running      bool               `json:"running"`
	Ready        bool               `json:"ready"`
	Degraded     bool               `json:"degraded"`
	CrashLoop    bool               `json:"crash_loop"`
	Incompatible bool               `json:"incompatible"`
	Lifecycle    lifecycle.Snapshot `json:"lifecycle"`
	Sidecars     []SidecarSnapshot  `json:"sidecars"`
}

func isNilManager(manager ProcessManager) bool {
	if manager == nil {
		return true
	}
	value := reflect.ValueOf(manager)
	return value.Kind() == reflect.Pointer && value.IsNil()
}
