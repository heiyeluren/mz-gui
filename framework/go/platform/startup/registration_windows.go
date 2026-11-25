//go:build windows

package startup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type WindowsRegistrationOptions struct {
	TaskName   string
	TaskFile   string
	Executable string
	UserSID    string
	Runner     CommandRunner
}

type WindowsRegistration struct {
	options WindowsRegistrationOptions
}

func NewWindowsRegistration(options WindowsRegistrationOptions) *WindowsRegistration {
	if options.Runner == nil {
		options.Runner = execCommandRunner{}
	}
	return &WindowsRegistration{options: options}
}

func (registration *WindowsRegistration) SetEnabled(ctx context.Context, enabled bool) error {
	if err := registration.validate(); err != nil {
		return err
	}
	if !enabled {
		_, _ = registration.options.Runner.Run(ctx, "schtasks.exe", "/End", "/TN", registration.options.TaskName)
		_, err := registration.options.Runner.Run(ctx, "schtasks.exe", "/Delete", "/TN", registration.options.TaskName, "/F")
		if err != nil && !isTaskMissingError(err) {
			return fmt.Errorf("移除计划任务: %w", err)
		}
		return nil
	}
	if err := ValidateRegularFile(registration.options.Executable, false); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(registration.options.TaskFile), 0o700); err != nil {
		return fmt.Errorf("创建计划任务状态目录: %w", err)
	}
	data, err := RenderWindowsTask(WindowsTaskSpec{Executable: registration.options.Executable, UserSID: registration.options.UserSID})
	if err != nil {
		return err
	}
	if err := WritePrivateAtomic(registration.options.TaskFile, data); err != nil {
		return err
	}
	if output, err := registration.options.Runner.Run(ctx, "schtasks.exe", "/Create", "/TN", registration.options.TaskName, "/XML", registration.options.TaskFile, "/F"); err != nil {
		return fmt.Errorf("注册计划任务: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	_, _ = registration.options.Runner.Run(ctx, "schtasks.exe", "/Run", "/TN", registration.options.TaskName)
	return nil
}

func (registration *WindowsRegistration) Status(ctx context.Context) (RegistrationStatus, error) {
	if err := registration.validate(); err != nil {
		return RegistrationStatus{}, err
	}
	data, err := registration.options.Runner.Run(ctx, "schtasks.exe", "/Query", "/TN", registration.options.TaskName, "/XML")
	if err != nil {
		if isTaskMissingError(err) {
			return RegistrationStatus{Strategy: "task-scheduler", Location: registration.options.TaskName}, nil
		}
		return RegistrationStatus{}, fmt.Errorf("查询计划任务: %w", err)
	}
	return ParseWindowsTaskStatus(data, registration.options.Executable, registration.options.TaskName)
}

func (registration *WindowsRegistration) validate() error {
	if strings.TrimSpace(registration.options.TaskName) == "" || !filepath.IsAbs(registration.options.TaskFile) || !isWindowsAbsolute(registration.options.Executable) || strings.TrimSpace(registration.options.UserSID) == "" {
		return errors.New("Windows 计划任务参数无效")
	}
	return nil
}

func isTaskMissingError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}
