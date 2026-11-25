package winremote

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
	PlatformWindowsX64 = "windows-x64"
	StatusPass         = "pass"
	StatusFail         = "fail"
	StepIdentify       = "identify"
	StepDeviceProbe    = "deviceprobe"
	StepFrameworkTest  = "framework-go-test"
	StepDesktopBuild   = "desktop-build"
)

var (
	ErrInvalidEvidence = errors.New("Windows 真机证据无效")
	ErrCommitMismatch  = errors.New("Windows 真机证据提交与期望不一致")
	ErrRemoteCollect   = errors.New("Windows 真机证据采集失败")
)

var requiredSteps = []string{StepIdentify, StepDeviceProbe, StepFrameworkTest, StepDesktopBuild}

type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type Evidence struct {
	SchemaVersion int    `json:"schema_version"`
	Platform      string `json:"platform"`
	GOOS          string `json:"goos"`
	GOARCH        string `json:"goarch"`
	Hostname      string `json:"hostname"`
	Commit        string `json:"commit"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at"`
	Status        string `json:"status"`
	Steps         []Step `json:"steps"`
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
	for _, step := range evidence.Steps {
		if step.Status != StatusPass {
			return false
		}
	}
	return true
}

func (evidence Evidence) validateSchema() error {
	if evidence.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schema_version 必须为 %d", ErrInvalidEvidence, SchemaVersion)
	}
	if evidence.Platform != PlatformWindowsX64 {
		return fmt.Errorf("%w: platform 必须为 %s", ErrInvalidEvidence, PlatformWindowsX64)
	}
	if evidence.GOOS != "windows" || evidence.GOARCH != "amd64" {
		return fmt.Errorf("%w: 仅接受 windows/amd64", ErrInvalidEvidence)
	}
	if strings.TrimSpace(evidence.Hostname) == "" {
		return fmt.Errorf("%w: hostname 不能为空", ErrInvalidEvidence)
	}
	if strings.TrimSpace(evidence.Commit) == "" {
		return fmt.Errorf("%w: commit 不能为空", ErrInvalidEvidence)
	}
	if evidence.Status != StatusPass && evidence.Status != StatusFail {
		return fmt.Errorf("%w: status 非法", ErrInvalidEvidence)
	}
	if _, err := time.Parse(time.RFC3339, evidence.StartedAt); err != nil {
		return fmt.Errorf("%w: started_at 非法", ErrInvalidEvidence)
	}
	if _, err := time.Parse(time.RFC3339, evidence.FinishedAt); err != nil {
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

func rejectTrailingJSON(decoder *json.Decoder) error {
	if decoder.More() {
		return fmt.Errorf("%w: 存在多余 JSON", ErrInvalidEvidence)
	}
	if err := decoder.Decode(&struct{}{}); err == nil {
		return fmt.Errorf("%w: 存在多余 JSON", ErrInvalidEvidence)
	}
	return nil
}
