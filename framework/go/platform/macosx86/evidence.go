package macosx86

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SchemaVersion      = 1
	PlatformMacosX8664 = "macos-x86_64"
	StatusPass         = "pass"
	StatusFail         = "fail"
	StepDeviceProbe    = "deviceprobe"
	StepDesktopBuild   = "desktop-build-cgo1"
	StepDesktopFile    = "desktop-file-cgo1"
)

var (
	ErrInvalidEvidence = errors.New("macos-x86_64 真机证据无效")
	requiredSteps      = []string{StepDeviceProbe, StepDesktopBuild, StepDesktopFile}
)

type Step struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Exit    *int     `json:"exit"`
	Seconds *float64 `json:"seconds"`
	Detail  string   `json:"detail"`
}

type Evidence struct {
	SchemaVersion   int      `json:"schema_version"`
	Platform        string   `json:"platform"`
	GOOS            string   `json:"goos"`
	GOARCH          string   `json:"goarch"`
	Hostname        string   `json:"hostname"`
	Username        string   `json:"username,omitempty"`
	HWModel         string   `json:"hw_model,omitempty"`
	CPU             string   `json:"cpu,omitempty"`
	SWVers          string   `json:"sw_vers,omitempty"`
	Uname           string   `json:"uname,omitempty"`
	GoVersion       string   `json:"go_version,omitempty"`
	CGOEnabledTests string   `json:"cgo_enabled_tests,omitempty"`
	StartedAt       string   `json:"started_at"`
	FinishedAt      string   `json:"finished_at"`
	Status          string   `json:"status"`
	FailedRequired  []string `json:"failed_required,omitempty"`
	Steps           []Step   `json:"steps"`
	Notes           []string `json:"notes,omitempty"`
}

func Decode(data []byte) (Evidence, error) {
	var evidence Evidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return Evidence{}, fmt.Errorf("%w: %v", ErrInvalidEvidence, err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Evidence{}, err
	}
	if err := evidence.validateSchema(); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

func (evidence Evidence) AllowsLocalAutomated() bool {
	if evidence.validateSchema() != nil {
		return false
	}
	if evidence.Status != StatusPass {
		return false
	}
	for _, name := range requiredSteps {
		step, ok := evidence.Step(name)
		if !ok || step.Status != StatusPass {
			return false
		}
	}
	return true
}

func (evidence Evidence) Step(name string) (Step, bool) {
	for _, step := range evidence.Steps {
		if step.Name == name {
			return step, true
		}
	}
	return Step{}, false
}

func (evidence Evidence) validateSchema() error {
	if evidence.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schema_version 必须为 %d", ErrInvalidEvidence, SchemaVersion)
	}
	if evidence.Platform != PlatformMacosX8664 {
		return fmt.Errorf("%w: platform 必须为 %s", ErrInvalidEvidence, PlatformMacosX8664)
	}
	if evidence.GOOS != "darwin" || evidence.GOARCH != "amd64" {
		return fmt.Errorf("%w: 仅接受 darwin/amd64", ErrInvalidEvidence)
	}
	if strings.TrimSpace(evidence.Hostname) == "" {
		return fmt.Errorf("%w: hostname 不能为空", ErrInvalidEvidence)
	}
	if evidence.Status != StatusPass && evidence.Status != StatusFail {
		return fmt.Errorf("%w: status 非法", ErrInvalidEvidence)
	}
	if err := parseTime(evidence.StartedAt); err != nil {
		return fmt.Errorf("%w: started_at 非法", ErrInvalidEvidence)
	}
	if err := parseTime(evidence.FinishedAt); err != nil {
		return fmt.Errorf("%w: finished_at 非法", ErrInvalidEvidence)
	}
	seen := make(map[string]Step, len(evidence.Steps))
	for _, step := range evidence.Steps {
		if step.Name == "" {
			return fmt.Errorf("%w: 步骤名不能为空", ErrInvalidEvidence)
		}
		if step.Status != StatusPass && step.Status != StatusFail {
			return fmt.Errorf("%w: 步骤状态非法: %s", ErrInvalidEvidence, step.Name)
		}
		if _, exists := seen[step.Name]; exists {
			return fmt.Errorf("%w: 步骤重复: %s", ErrInvalidEvidence, step.Name)
		}
		seen[step.Name] = step
	}
	for _, name := range requiredSteps {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("%w: 缺少步骤 %s", ErrInvalidEvidence, name)
		}
	}
	return nil
}

func parseTime(value string) error {
	if _, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return nil
	}
	_, err := time.Parse(time.RFC3339, value)
	return err
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	if decoder.More() {
		return fmt.Errorf("%w: 存在多余 JSON", ErrInvalidEvidence)
	}
	if err := decoder.Decode(&struct{}{}); err == nil {
		return fmt.Errorf("%w: 存在多余 JSON", ErrInvalidEvidence)
	}
	return nil
}
