package nativee2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const maximumCapturedOutput = 1 << 20

type CommandProbe struct {
	ProbeID string
	Command Command
	Timeout time.Duration
}

func (probe CommandProbe) Name() string { return probe.ProbeID }

func (probe CommandProbe) Check(ctx context.Context) (string, error) {
	result, err := runCommand(ctx, probe.Command, probe.Timeout)
	return strings.TrimSpace(result.output()), err
}

type CommandRouteDriver struct {
	Command Command
	Timeout time.Duration
}

func (driver CommandRouteDriver) Visit(ctx context.Context, route string) (string, error) {
	command := driver.Command
	command.Args = append([]string(nil), command.Args...)
	found := false
	for index, arg := range command.Args {
		if strings.Contains(arg, "{route}") {
			command.Args[index] = strings.ReplaceAll(arg, "{route}", route)
			found = true
		}
	}
	if !found {
		command.Args = append(command.Args, route)
	}
	result, err := runCommand(ctx, command, driver.Timeout)
	if err != nil {
		return strings.TrimSpace(result.output()), err
	}
	var evidence RouteEvidence
	if decodeErr := json.Unmarshal([]byte(strings.TrimSpace(result.stdout)), &evidence); decodeErr != nil {
		return strings.TrimSpace(result.output()), fmt.Errorf("路由驱动未返回合法 JSON 证据: %w", decodeErr)
	}
	if evidence.SchemaVersion != 1 || evidence.Status != StatusPassed || evidence.RequestedRouteID != route || evidence.ObservedRouteID != route {
		return strings.TrimSpace(result.output()), fmt.Errorf("路由驱动证据不匹配: requested=%q observed=%q status=%q", evidence.RequestedRouteID, evidence.ObservedRouteID, evidence.Status)
	}
	detail := strings.TrimSpace(evidence.Detail)
	if detail == "" {
		detail = "已由真实 UI 驱动观测路由 " + evidence.ObservedRouteID
	}
	return detail, nil
}

type commandResult struct {
	stdout string
	stderr string
}

func (result commandResult) output() string {
	values := make([]string, 0, 2)
	if value := strings.TrimSpace(result.stdout); value != "" {
		values = append(values, value)
	}
	if value := strings.TrimSpace(result.stderr); value != "" {
		values = append(values, value)
	}
	return strings.Join(values, "\n")
}

func runCommand(parent context.Context, command Command, timeout time.Duration) (commandResult, error) {
	if strings.TrimSpace(command.Executable) == "" {
		return commandResult{}, fmt.Errorf("%w: 命令主程序为空", ErrInvalidConfig)
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command.Executable, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = mergedEnvironment(command.Env)
	stdout := newBoundedBuffer(maximumCapturedOutput)
	stderr := newBoundedBuffer(maximumCapturedOutput)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, fmt.Errorf("命令超时: %w", ctx.Err())
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return result, fmt.Errorf("命令退出码 %d", exitErr.ExitCode())
	}
	return result, err
}

func mergedEnvironment(values map[string]string) []string {
	environment := append([]string(nil), os.Environ()...)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}

type boundedBuffer struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func newBoundedBuffer(limit int) *boundedBuffer { return &boundedBuffer{limit: limit} }

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	written := len(data)
	remaining := buffer.limit - len(buffer.data)
	if remaining <= 0 {
		buffer.truncated = true
		return written, nil
	}
	if len(data) > remaining {
		data = data[:remaining]
		buffer.truncated = true
	}
	buffer.data = append(buffer.data, data...)
	return written, nil
}

func (buffer *boundedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(append([]byte(nil), buffer.data...))
}

func (buffer *boundedBuffer) Truncated() bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.truncated
}
