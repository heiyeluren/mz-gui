package nativee2e

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
)

func Run(ctx context.Context, config Config) (report Report, resultErr error) {
	config = withDefaults(config)
	startedAt := config.Now().UTC()
	report = Report{
		SchemaVersion: 1,
		RunID:         fmt.Sprintf("native-e2e-%d", startedAt.UnixNano()),
		Status:        StatusFailed,
		Platform:      config.TargetOS,
		Architecture:  runtime.GOARCH,
		Artifact:      config.Artifact,
		StartedAt:     startedAt,
		UIAutomation:  config.UIAutomation,
	}
	defer func() {
		report.FinishedAt = config.Now().UTC()
		report.DurationMS = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	}()
	if err := validateConfig(config); err != nil {
		appendFailure(&report, err)
		return report, err
	}
	executable, err := ResolveArtifactExecutable(config.TargetOS, config.Artifact)
	if err != nil {
		appendFailure(&report, err)
		return report, err
	}
	report.Desktop = ProcessResult{Name: config.DesktopName, Executable: executable, Status: StatusFailed}
	process, err := startProcess(ctx, executable, config)
	if err != nil {
		appendFailure(&report, fmt.Errorf("启动桌面制品: %w", err))
		return report, ErrValidationFailed
	}
	report.Desktop.PID = process.PID()
	defer func() {
		if err := processExitError(process); err != nil && report.Desktop.Status != StatusFailed {
			report.Desktop.Status = StatusFailed
			appendFailure(&report, fmt.Errorf("报告完成前 desktop 已退出: %w", err))
			resultErr = ErrValidationFailed
		}
		report.Desktop.StoppedByHarness = process.stop()
		report.Desktop.ExitCode = process.exitCode()
		report.ProcessOutput = ProcessOutput{
			Stdout: process.stdout.String(), Stderr: process.stderr.String(),
			Truncated: process.stdout.Truncated() || process.stderr.Truncated(),
		}
	}()

	desktopCheck := waitForDesktop(ctx, process, config)
	report.HealthChecks = append(report.HealthChecks, desktopCheck)
	if desktopCheck.Status == StatusPassed {
		report.Desktop.Status = StatusPassed
	} else {
		appendFailure(&report, errors.New(desktopCheck.Error))
		return report, ErrValidationFailed
	}

	startupCtx, cancelStartup := context.WithTimeout(ctx, config.StartupTimeout)
	defer cancelStartup()
	for _, probe := range config.HealthProbes {
		check := waitForProbe(startupCtx, probe, "health", config)
		report.HealthChecks = append(report.HealthChecks, check)
		if check.Status == StatusFailed {
			appendFailure(&report, fmt.Errorf("健康检查 %s: %s", check.ID, check.Error))
		}
	}
	if report.FailureCount > 0 {
		return report, ErrValidationFailed
	}

	for _, probe := range config.RPCProbes {
		check := checkProbe(ctx, probe, "rpc", config)
		report.RPCChecks = append(report.RPCChecks, check)
		if check.Status == StatusFailed {
			appendFailure(&report, fmt.Errorf("RPC %s: %s", check.ID, check.Error))
		}
	}
	for _, route := range config.Routes {
		if err := processExitError(process); err != nil {
			result := failedRoute(route, config.Now().UTC(), fmt.Errorf("desktop 已退出: %w", err))
			report.Routes = append(report.Routes, result)
			appendFailure(&report, fmt.Errorf("路由 %s: %s", route, result.Error))
			report.Desktop.Status = StatusFailed
			continue
		}
		result := visitRoute(ctx, config.RouteDriver, route, config)
		report.Routes = append(report.Routes, result)
		if result.Status == StatusFailed {
			appendFailure(&report, fmt.Errorf("路由 %s: %s", route, result.Error))
		}
	}
	if err := processExitError(process); err != nil && report.Desktop.Status != StatusFailed {
		report.Desktop.Status = StatusFailed
		appendFailure(&report, fmt.Errorf("路由验证结束前 desktop 已退出: %w", err))
	}
	if report.FailureCount > 0 {
		return report, ErrValidationFailed
	}
	report.Status = StatusPassed
	return report, nil
}

func withDefaults(config Config) Config {
	if config.TargetOS == "" {
		config.TargetOS = runtime.GOOS
	}
	if config.DesktopName == "" {
		config.DesktopName = "desktop"
	}
	if config.DesktopStability <= 0 {
		config.DesktopStability = 2 * time.Second
	}
	if config.StartupTimeout <= 0 {
		config.StartupTimeout = 30 * time.Second
	}
	if config.ProbeInterval <= 0 {
		config.ProbeInterval = 200 * time.Millisecond
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.UIAutomation.Mode == "" {
		config.UIAutomation.Mode = "external-command"
	}
	config.UIAutomation.Configured = config.RouteDriver != nil
	if !config.UIAutomation.Configured && config.UIAutomation.Remaining == "" {
		config.UIAutomation.Remaining = "当前生产 WebView 未暴露可外部回读 DOM 的原生驱动；需接入平台 UI Automation 驱动"
	}
	return config
}

func validateConfig(config Config) error {
	if !filepathLikeAbsolute(config.Artifact) || strings.TrimSpace(config.DesktopName) == "" || len(config.HealthProbes) == 0 {
		return ErrInvalidConfig
	}
	for _, probe := range append(append([]Probe(nil), config.HealthProbes...), config.RPCProbes...) {
		if probe == nil || strings.TrimSpace(probe.Name()) == "" {
			return ErrInvalidConfig
		}
	}
	seen := make(map[string]struct{}, len(config.Routes))
	for _, route := range config.Routes {
		if strings.TrimSpace(route) == "" {
			return ErrInvalidConfig
		}
		if _, exists := seen[route]; exists {
			return fmt.Errorf("%w: 正式路由重复 %s", ErrInvalidConfig, route)
		}
		seen[route] = struct{}{}
	}
	if len(config.Routes) > 0 && config.RouteDriver == nil {
		return ErrRouteDriverRequired
	}
	return nil
}

func filepathLikeAbsolute(path string) bool {
	if path == "" {
		return false
	}
	if len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	return strings.HasPrefix(path, "/")
}

func waitForDesktop(ctx context.Context, process *managedProcess, config Config) CheckResult {
	startedAt := config.Now().UTC()
	timer := time.NewTimer(config.DesktopStability)
	defer timer.Stop()
	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case <-process.done:
		err = processExitError(process)
	case <-timer.C:
	}
	return newCheck("desktop.process", "process", startedAt, config.Now().UTC(), "进程在稳定窗口内持续运行", err)
}

func waitForProbe(ctx context.Context, probe Probe, kind string, config Config) CheckResult {
	startedAt := config.Now().UTC()
	var detail string
	var err error
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, minDuration(2*time.Second, config.StartupTimeout))
		detail, err = probe.Check(attemptCtx)
		cancel()
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			err = errors.Join(err, ctx.Err())
			return newCheck(probe.Name(), kind, startedAt, config.Now().UTC(), detail, err)
		case <-time.After(config.ProbeInterval):
		}
	}
	return newCheck(probe.Name(), kind, startedAt, config.Now().UTC(), detail, nil)
}

func checkProbe(ctx context.Context, probe Probe, kind string, config Config) CheckResult {
	startedAt := config.Now().UTC()
	detail, err := probe.Check(ctx)
	return newCheck(probe.Name(), kind, startedAt, config.Now().UTC(), detail, err)
}

func visitRoute(ctx context.Context, driver RouteDriver, route string, config Config) RouteResult {
	startedAt := config.Now().UTC()
	detail, err := driver.Visit(ctx, route)
	finishedAt := config.Now().UTC()
	result := RouteResult{RouteID: route, Status: StatusPassed, StartedAt: startedAt, FinishedAt: finishedAt, DurationMS: finishedAt.Sub(startedAt).Milliseconds(), Detail: detail}
	if err != nil {
		result.Status = StatusFailed
		result.Error = err.Error()
	}
	return result
}

func failedRoute(route string, observedAt time.Time, err error) RouteResult {
	return RouteResult{RouteID: route, Status: StatusFailed, StartedAt: observedAt, FinishedAt: observedAt, Error: err.Error()}
}

func newCheck(id, kind string, startedAt, finishedAt time.Time, detail string, err error) CheckResult {
	result := CheckResult{ID: id, Kind: kind, Status: StatusPassed, StartedAt: startedAt, FinishedAt: finishedAt, DurationMS: finishedAt.Sub(startedAt).Milliseconds(), Detail: detail}
	if err != nil {
		result.Status = StatusFailed
		result.Error = err.Error()
	}
	return result
}

func appendFailure(report *Report, err error) {
	report.Status = StatusFailed
	report.FailureCount++
	report.Failures = append(report.Failures, err.Error())
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
