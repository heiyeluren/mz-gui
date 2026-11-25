package startup

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var launchAgentLabelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,127}$`)

type LaunchAgentSpec struct {
	Label      string
	Executable string
	StdoutPath string
	StderrPath string
}

func RenderLaunchAgent(spec LaunchAgentSpec) ([]byte, error) {
	if !launchAgentLabelPattern.MatchString(spec.Label) {
		return nil, errors.New("LaunchAgent label 无效")
	}
	for _, value := range []string{spec.Executable, spec.StdoutPath, spec.StderrPath} {
		if !filepath.IsAbs(value) {
			return nil, errors.New("LaunchAgent 路径必须是绝对路径")
		}
	}
	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)
	buffer.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	buffer.WriteString(`<plist version="1.0"><dict>`)
	writePlistString(&buffer, "Label", spec.Label)
	buffer.WriteString(`<key>ProgramArguments</key><array><string>`)
	_ = xml.EscapeText(&buffer, []byte(filepath.Clean(spec.Executable)))
	buffer.WriteString(`</string></array>`)
	buffer.WriteString(`<key>RunAtLoad</key><true/><key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>`)
	writePlistString(&buffer, "StandardOutPath", filepath.Clean(spec.StdoutPath))
	writePlistString(&buffer, "StandardErrorPath", filepath.Clean(spec.StderrPath))
	buffer.WriteString(`</dict></plist>` + "\n")
	return buffer.Bytes(), nil
}

func writePlistString(buffer *bytes.Buffer, key, value string) {
	buffer.WriteString("<key>")
	_ = xml.EscapeText(buffer, []byte(key))
	buffer.WriteString("</key><string>")
	_ = xml.EscapeText(buffer, []byte(value))
	buffer.WriteString("</string>")
}

type WindowsTaskSpec struct {
	Executable string
	UserSID    string
}

type windowsTaskDocument struct {
	XMLName    xml.Name              `xml:"Task"`
	Version    string                `xml:"version,attr"`
	Xmlns      string                `xml:"xmlns,attr"`
	Principals windowsTaskPrincipals `xml:"Principals"`
	Triggers   windowsTaskTriggers   `xml:"Triggers"`
	Settings   windowsTaskSettings   `xml:"Settings"`
	Actions    windowsTaskActions    `xml:"Actions"`
}

type windowsTaskPrincipals struct {
	Principal windowsTaskPrincipal `xml:"Principal"`
}

type windowsTaskPrincipal struct {
	ID        string `xml:"id,attr"`
	UserID    string `xml:"UserId"`
	LogonType string `xml:"LogonType"`
	RunLevel  string `xml:"RunLevel"`
}

type windowsTaskTriggers struct {
	Logon windowsTaskLogonTrigger `xml:"LogonTrigger"`
}

type windowsTaskLogonTrigger struct {
	Enabled bool `xml:"Enabled"`
}

type windowsTaskSettings struct {
	MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
	DisallowStartIfOnBatteries bool   `xml:"DisallowStartIfOnBatteries"`
	StopIfGoingOnBatteries     bool   `xml:"StopIfGoingOnBatteries"`
	StartWhenAvailable         bool   `xml:"StartWhenAvailable"`
	ExecutionTimeLimit         string `xml:"ExecutionTimeLimit"`
}

type windowsTaskActions struct {
	Context string          `xml:"Context,attr"`
	Exec    windowsTaskExec `xml:"Exec"`
}

type windowsTaskExec struct {
	Command string `xml:"Command"`
}

func RenderWindowsTask(spec WindowsTaskSpec) ([]byte, error) {
	if !isWindowsAbsolute(spec.Executable) || strings.TrimSpace(spec.UserSID) == "" {
		return nil, errors.New("Windows 计划任务参数无效")
	}
	document := windowsTaskDocument{
		Version: "1.4", Xmlns: "http://schemas.microsoft.com/windows/2004/02/mit/task",
		Principals: windowsTaskPrincipals{Principal: windowsTaskPrincipal{ID: "Author", UserID: spec.UserSID, LogonType: "InteractiveToken", RunLevel: "LeastPrivilege"}},
		Triggers:   windowsTaskTriggers{Logon: windowsTaskLogonTrigger{Enabled: true}},
		Settings:   windowsTaskSettings{MultipleInstancesPolicy: "IgnoreNew", StartWhenAvailable: true, ExecutionTimeLimit: "PT0S"},
		Actions:    windowsTaskActions{Context: "Author", Exec: windowsTaskExec{Command: spec.Executable}},
	}
	data, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("编码 Windows 计划任务: %w", err)
	}
	return append([]byte(xml.Header), append(data, '\n')...), nil
}

func ParseWindowsTaskStatus(data []byte, currentExecutable, location string) (RegistrationStatus, error) {
	var document windowsTaskDocument
	if err := xml.Unmarshal(data, &document); err != nil {
		return RegistrationStatus{}, fmt.Errorf("解析 Windows 计划任务: %w", err)
	}
	executable := strings.TrimSpace(document.Actions.Exec.Command)
	if executable == "" {
		return RegistrationStatus{}, errors.New("Windows 计划任务缺少可执行文件")
	}
	return RegistrationStatus{
		Enabled: true, Drifted: !strings.EqualFold(filepath.Clean(executable), filepath.Clean(currentExecutable)),
		Strategy: "task-scheduler", Location: location, Executable: executable,
	}, nil
}

func isWindowsAbsolute(path string) bool {
	return len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}
