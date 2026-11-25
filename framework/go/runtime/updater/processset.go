package updater

import (
	"errors"
	"strings"
)

const (
	ComponentDesktop = "desktop"
	ComponentAgentd  = "agentd"
	ComponentHelper  = "helper"
)

var (
	ErrVersionMismatch   = errors.New("进程版本不一致")
	ErrInvalidProcessSet = errors.New("进程集合无效")
)

type ProcessArtifact struct {
	Name    string
	Version string
	BuildID string
}

type ProcessSet struct {
	Desktop  ProcessArtifact
	Sidecars []ProcessArtifact
}

func CheckProcessSet(set ProcessSet) error {
	if strings.TrimSpace(set.Desktop.Name) == "" {
		return ErrInvalidProcessSet
	}
	if _, err := parseVersion(set.Desktop.Version); err != nil {
		return err
	}
	for _, sidecar := range set.Sidecars {
		if strings.TrimSpace(sidecar.Name) == "" {
			return ErrInvalidProcessSet
		}
		if _, err := parseVersion(sidecar.Version); err != nil {
			return err
		}
		if sidecar.Version != set.Desktop.Version {
			return ErrVersionMismatch
		}
		if set.Desktop.BuildID != "" && sidecar.BuildID != set.Desktop.BuildID {
			return ErrVersionMismatch
		}
	}
	return nil
}

// RequireStartableProcessSet 拒绝新 desktop 与旧 agentd 混跑；启动必须包含同版本 agentd。
func RequireStartableProcessSet(set ProcessSet) error {
	if err := CheckProcessSet(set); err != nil {
		return err
	}
	for _, sidecar := range set.Sidecars {
		if sidecar.Name == ComponentAgentd {
			return nil
		}
	}
	return ErrInvalidProcessSet
}

func validProcessComponent(value string) bool {
	switch strings.TrimSpace(value) {
	case "", ComponentDesktop, ComponentAgentd, ComponentHelper:
		return true
	default:
		return false
	}
}
