//go:build darwin

package startup

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type DarwinRegistrationOptions struct {
	Label        string
	LegacyLabels []string
	Home         string
	Executable   string
	StdoutPath   string
	StderrPath   string
	Runner       CommandRunner
	UID          int
}

type DarwinRegistration struct {
	options DarwinRegistrationOptions
}

func NewDarwinRegistration(options DarwinRegistrationOptions) *DarwinRegistration {
	if options.Runner == nil {
		options.Runner = execCommandRunner{}
	}
	return &DarwinRegistration{options: options}
}

func (registration *DarwinRegistration) plistPath() string {
	return registration.plistPathForLabel(registration.options.Label)
}

func (registration *DarwinRegistration) plistPathForLabel(label string) string {
	return filepath.Join(registration.options.Home, "Library", "LaunchAgents", label+".plist")
}

func (registration *DarwinRegistration) SetEnabled(ctx context.Context, enabled bool) error {
	if err := registration.validate(); err != nil {
		return err
	}
	target := "gui/" + strconv.Itoa(registration.options.UID)
	if !enabled {
		labels := append([]string{registration.options.Label}, registration.options.LegacyLabels...)
		for _, label := range labels {
			if err := registration.disableLabel(ctx, target, label); err != nil {
				return err
			}
		}
		return nil
	}
	if err := registration.MigrateLegacy(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(registration.plistPath()), 0o700); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录: %w", err)
	}
	for _, path := range []string{registration.options.StdoutPath, registration.options.StderrPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("创建 sidecar 日志目录: %w", err)
		}
	}
	data, err := RenderLaunchAgent(LaunchAgentSpec{
		Label: registration.options.Label, Executable: registration.options.Executable,
		StdoutPath: registration.options.StdoutPath, StderrPath: registration.options.StderrPath,
	})
	if err != nil {
		return err
	}
	if err := WritePrivateAtomic(registration.plistPath(), data); err != nil {
		return err
	}
	service := target + "/" + registration.options.Label
	if _, err := registration.options.Runner.Run(ctx, "launchctl", "bootstrap", target, registration.plistPath()); err != nil {
		_, _ = registration.options.Runner.Run(ctx, "launchctl", "bootout", service)
		if retryErr := registration.bootstrapAfterBootout(ctx, target); retryErr != nil {
			return fmt.Errorf("加载 LaunchAgent: %w", retryErr)
		}
	}
	_, _ = registration.options.Runner.Run(ctx, "launchctl", "kickstart", "-k", service)
	return nil
}

func (registration *DarwinRegistration) bootstrapAfterBootout(ctx context.Context, target string) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		if _, err := registration.options.Runner.Run(ctx, "launchctl", "bootstrap", target, registration.plistPath()); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

// MigrateLegacy 只停用调用方明确声明的旧注册，不改变当前注册的启用状态。
func (registration *DarwinRegistration) MigrateLegacy(ctx context.Context) error {
	if err := registration.validate(); err != nil {
		return err
	}
	if err := ValidateRegularFile(registration.options.Executable, true); err != nil {
		return err
	}
	target := "gui/" + strconv.Itoa(registration.options.UID)
	for _, label := range registration.options.LegacyLabels {
		if err := registration.disableLabel(ctx, target, label); err != nil {
			return err
		}
	}
	return nil
}

func (registration *DarwinRegistration) disableLabel(ctx context.Context, target, label string) error {
	_, _ = registration.options.Runner.Run(ctx, "launchctl", "bootout", target+"/"+label)
	if err := os.Remove(registration.plistPathForLabel(label)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("移除 LaunchAgent %s: %w", label, err)
	}
	return nil
}

func (registration *DarwinRegistration) Status(context.Context) (RegistrationStatus, error) {
	if err := registration.validate(); err != nil {
		return RegistrationStatus{}, err
	}
	data, err := os.ReadFile(registration.plistPath())
	if os.IsNotExist(err) {
		return RegistrationStatus{Strategy: "launchagent", Location: registration.plistPath()}, nil
	}
	if err != nil {
		return RegistrationStatus{}, fmt.Errorf("读取 LaunchAgent: %w", err)
	}
	executable, err := plistProgramArgument(data)
	if err != nil {
		return RegistrationStatus{}, err
	}
	return RegistrationStatus{
		Enabled: true, Drifted: filepath.Clean(executable) != filepath.Clean(registration.options.Executable),
		Strategy: "launchagent", Location: registration.plistPath(), Executable: executable,
	}, nil
}

func (registration *DarwinRegistration) validate() error {
	if !launchAgentLabelPattern.MatchString(registration.options.Label) {
		return errors.New("LaunchAgent label 无效")
	}
	for _, label := range registration.options.LegacyLabels {
		if !launchAgentLabelPattern.MatchString(label) || label == registration.options.Label {
			return errors.New("旧 LaunchAgent label 无效")
		}
	}
	if !filepath.IsAbs(registration.options.Home) || !filepath.IsAbs(registration.options.Executable) || !filepath.IsAbs(registration.options.StdoutPath) || !filepath.IsAbs(registration.options.StderrPath) {
		return errors.New("LaunchAgent 路径必须是绝对路径")
	}
	if registration.options.UID < 0 {
		return errors.New("LaunchAgent UID 无效")
	}
	return nil
}

func plistProgramArgument(data []byte) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	wantsProgram := false
	inArguments := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("解析 LaunchAgent: %w", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "key" {
				var key string
				if err := decoder.DecodeElement(&key, &value); err != nil {
					return "", err
				}
				wantsProgram = key == "ProgramArguments"
			} else if value.Name.Local == "array" && wantsProgram {
				inArguments = true
			} else if value.Name.Local == "string" && inArguments {
				var executable string
				if err := decoder.DecodeElement(&executable, &value); err != nil {
					return "", err
				}
				if strings.TrimSpace(executable) != "" {
					return executable, nil
				}
			}
		case xml.EndElement:
			if value.Name.Local == "array" {
				inArguments = false
			}
		}
	}
	return "", errors.New("LaunchAgent 缺少 ProgramArguments")
}
