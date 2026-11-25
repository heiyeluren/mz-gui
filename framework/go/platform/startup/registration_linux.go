//go:build linux

package startup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type LinuxRegistrationOptions struct {
	Unit, Directory, Executable string
	Environment                 map[string]string
	Runner                      CommandRunner
}

type LinuxRegistration struct{ options LinuxRegistrationOptions }

func NewLinuxRegistration(options LinuxRegistrationOptions) *LinuxRegistration {
	if options.Runner == nil {
		options.Runner = execCommandRunner{}
	}
	return &LinuxRegistration{options: options}
}

func (r *LinuxRegistration) document() ([]byte, error) {
	o := r.options
	if !sidecarNamePattern.MatchString(o.Unit) || !strings.HasSuffix(o.Unit, ".service") || !filepath.IsAbs(o.Directory) || !filepath.IsAbs(o.Executable) || strings.ContainsAny(o.Executable, "\n\r\x00") {
		return nil, errors.New("systemd 用户服务参数无效")
	}
	// systemd 的百分号和美元符号不是 shell 参数，必须单独转义。
	quote := func(s string) string {
		return strconv.Quote(strings.ReplaceAll(strings.ReplaceAll(s, "%", "%%"), "$", "$$"))
	}
	text := "[Unit]\nDescription=Desktop background service\n[Service]\nType=simple\nExecStart=" + quote(o.Executable) + "\nRestart=on-failure\nRestartSec=3\n"
	keys := make([]string, 0, len(o.Environment))
	for key := range o.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := o.Environment[key]
		if value == "" {
			continue
		}
		if strings.ContainsAny(key, "=\n\r\x00") || strings.ContainsAny(value, "\n\r\x00") {
			return nil, errors.New("systemd 环境变量无效")
		}
		text += "Environment=" + strconv.Quote(strings.ReplaceAll(key+"="+value, "%", "%%")) + "\n"
	}
	return []byte(text + "[Install]\nWantedBy=default.target\n"), nil
}

func (r *LinuxRegistration) SetEnabled(ctx context.Context, enabled bool) error {
	data, err := r.document()
	if err != nil {
		return err
	}
	path := filepath.Join(r.options.Directory, r.options.Unit)
	if !enabled {
		if _, err := r.options.Runner.Run(ctx, "systemctl", "--user", "disable", "--now", r.options.Unit); err != nil {
			return fmt.Errorf("停用 systemd 用户服务: %w", err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		if err := ValidateRegularFile(r.options.Executable, true); err != nil {
			return err
		}
		if err := os.MkdirAll(r.options.Directory, 0700); err != nil {
			return err
		}
		if err := WritePrivateAtomic(path, data); err != nil {
			return err
		}
	}
	if _, err := r.options.Runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if enabled {
		_, err = r.options.Runner.Run(ctx, "systemctl", "--user", "enable", "--now", r.options.Unit)
	}
	return err
}

func (r *LinuxRegistration) Status(ctx context.Context) (RegistrationStatus, error) {
	expected, err := r.document()
	if err != nil {
		return RegistrationStatus{}, err
	}
	path := filepath.Join(r.options.Directory, r.options.Unit)
	status := RegistrationStatus{Strategy: "systemd-user", Location: path, Executable: r.options.Executable}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	output, err := r.options.Runner.Run(ctx, "systemctl", "--user", "is-enabled", r.options.Unit)
	state := strings.TrimSpace(string(output))
	if err != nil && state != "disabled" {
		return status, fmt.Errorf("查询 systemd 用户服务: %w", err)
	}
	status.Enabled = state == "enabled"
	status.Drifted = string(data) != string(expected)
	return status, nil
}
