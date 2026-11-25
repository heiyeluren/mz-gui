package nativee2e

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunnerStartsRealProcessChecksRPCAndVisitsEveryRoute(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	routeLog := filepath.Join(root, "routes.log")
	health := helperCommand(executable, "probe", "health", "pass")
	rpc := helperCommand(executable, "probe", "bootstrap", "pass")
	driver := CommandRouteDriver{
		Command: helperCommand(executable, "route", "{route}", routeLog),
		Timeout: 2 * time.Second,
	}

	report, runErr := Run(context.Background(), Config{
		Artifact:         executable,
		ArtifactArgs:     helperArgs("desktop"),
		DesktopName:      "desktop",
		DesktopStability: 30 * time.Millisecond,
		StartupTimeout:   5 * time.Second,
		ProbeInterval:    10 * time.Millisecond,
		HealthProbes: []Probe{
			CommandProbe{ProbeID: "agentd.grpc-health", Command: health, Timeout: time.Second},
		},
		RPCProbes: []Probe{
			CommandProbe{ProbeID: "agentd.bootstrap", Command: rpc, Timeout: time.Second},
		},
		Routes:      []string{"workspace.chat", "view.terminal"},
		RouteDriver: driver,
	})
	if runErr != nil {
		t.Fatalf("真实子进程 E2E 应通过: %v\n%#v", runErr, report)
	}
	if report.Status != StatusPassed || report.Desktop.PID <= 0 || len(report.HealthChecks) != 2 || len(report.RPCChecks) != 1 {
		t.Fatalf("报告摘要不完整: %#v", report)
	}
	if len(report.Routes) != 2 || report.Routes[0].RouteID != "workspace.chat" || report.Routes[1].RouteID != "view.terminal" {
		t.Fatalf("正式路由未按顺序遍历: %#v", report.Routes)
	}
	data, err := os.ReadFile(routeLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(data)); strings.Join(got, ",") != "workspace.chat,view.terminal" {
		t.Fatalf("真实路由驱动收到的路由错误: %q", data)
	}
}

func TestRunnerAggregatesRPCAndRouteFailures(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	report, runErr := Run(context.Background(), Config{
		Artifact:         executable,
		ArtifactArgs:     helperArgs("desktop"),
		DesktopName:      "desktop",
		DesktopStability: 20 * time.Millisecond,
		StartupTimeout:   5 * time.Second,
		ProbeInterval:    10 * time.Millisecond,
		HealthProbes: []Probe{
			CommandProbe{ProbeID: "agentd.grpc-health", Command: helperCommand(executable, "probe", "health", "pass"), Timeout: time.Second},
		},
		RPCProbes: []Probe{
			CommandProbe{ProbeID: "agentd.bootstrap", Command: helperCommand(executable, "probe", "bootstrap", "fail"), Timeout: time.Second},
		},
		Routes: []string{"workspace.chat", "view.terminal"},
		RouteDriver: CommandRouteDriver{
			Command: helperCommand(executable, "route-fail", "{route}"),
			Timeout: time.Second,
		},
	})
	if runErr == nil || !errors.Is(runErr, ErrValidationFailed) {
		t.Fatalf("RPC/路由失败必须令运行失败: %v", runErr)
	}
	if report.Status != StatusFailed || len(report.RPCChecks) != 1 || report.RPCChecks[0].Status != StatusFailed {
		t.Fatalf("RPC 失败未进入报告: %#v", report)
	}
	if len(report.Routes) != 2 || report.Routes[0].Status != StatusFailed || report.Routes[1].Status != StatusFailed {
		t.Fatalf("失败后仍须遍历并记录全部路由: %#v", report.Routes)
	}
}

func TestRunnerFailsWhenDesktopExitsBeforeStabilityWindow(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	report, runErr := Run(context.Background(), Config{
		Artifact:         executable,
		ArtifactArgs:     helperArgs("crash"),
		DesktopName:      "desktop",
		DesktopStability: 200 * time.Millisecond,
		StartupTimeout:   time.Second,
		HealthProbes: []Probe{
			CommandProbe{ProbeID: "agentd.grpc-health", Command: helperCommand(executable, "probe", "health", "pass")},
		},
	})
	if !errors.Is(runErr, ErrValidationFailed) || len(report.HealthChecks) != 1 || report.HealthChecks[0].Status != StatusFailed {
		t.Fatalf("桌面提前退出必须失败: report=%#v err=%v", report, runErr)
	}
}

func TestRunnerFailsWhenAgentdHealthTimesOut(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	report, runErr := Run(context.Background(), Config{
		Artifact:         executable,
		ArtifactArgs:     helperArgs("desktop"),
		DesktopName:      "desktop",
		DesktopStability: 20 * time.Millisecond,
		StartupTimeout:   80 * time.Millisecond,
		ProbeInterval:    10 * time.Millisecond,
		HealthProbes: []Probe{
			CommandProbe{ProbeID: "agentd.grpc-health", Command: helperCommand(executable, "probe", "health", "fail"), Timeout: 40 * time.Millisecond},
		},
	})
	if !errors.Is(runErr, ErrValidationFailed) || len(report.HealthChecks) != 2 || report.HealthChecks[1].Status != StatusFailed {
		t.Fatalf("agentd 健康超时必须失败: report=%#v err=%v", report, runErr)
	}
}

func TestRunnerRejectsRoutesWithoutRealDriver(t *testing.T) {
	_, err := Run(context.Background(), Config{
		Artifact:     filepath.Join(t.TempDir(), "Mozi APP.app"),
		DesktopName:  "desktop",
		HealthProbes: []Probe{CommandProbe{ProbeID: "agentd.health", Command: Command{Executable: "missing"}}},
		Routes:       []string{"workspace.chat"},
	})
	if !errors.Is(err, ErrRouteDriverRequired) {
		t.Fatalf("存在正式路由时必须要求真实驱动: %v", err)
	}
}

func TestCommandRouteDriverRejectsEmptyOrMismatchedEvidence(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		mode string
	}{
		{name: "空输出", mode: "route-empty"},
		{name: "观测路由不一致", mode: "route-mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver := CommandRouteDriver{Command: helperCommand(executable, test.mode, "{route}"), Timeout: time.Second}
			if _, err := driver.Visit(context.Background(), "workspace.chat"); err == nil {
				t.Fatal("无真实匹配证据的驱动必须失败")
			}
		})
	}
}

func TestResolveArtifactExecutableSupportsAppBundleAndWindowsEXE(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "Mozi APP.app")
	macOSDir := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macOSDir, 0o700); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>mz-gui-desktop</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o600); err != nil {
		t.Fatal(err)
	}
	desktop := filepath.Join(macOSDir, "mz-gui-desktop")
	if err := os.WriteFile(desktop, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveArtifactExecutable("darwin", app)
	if err != nil || resolved != desktop {
		t.Fatalf(".app 主程序解析失败: %q %v", resolved, err)
	}
	exe := filepath.Join(root, "Mozi APP.exe")
	if err := os.WriteFile(exe, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err = ResolveArtifactExecutable("windows", exe)
	if err != nil || resolved != exe {
		t.Fatalf("Windows EXE 解析失败: %q %v", resolved, err)
	}
}

func TestWriteReportPersistsMachineReadableFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native-e2e.json")
	report := Report{SchemaVersion: 1, RunID: "run-1", Status: StatusFailed, FailureCount: 2}
	if err := WriteReport(path, report); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("报告必须是合法 JSON: %v", err)
	}
	if decoded.Status != StatusFailed || decoded.FailureCount != 2 {
		t.Fatalf("报告回读不一致: %#v", decoded)
	}
}

func helperCommand(executable string, args ...string) Command {
	return Command{Executable: executable, Args: append(helperArgs(), args...), Env: map[string]string{"GORACE": "atexit_sleep_ms=0"}}
}

func helperArgs(args ...string) []string {
	return append([]string{"-test.run=TestNativeE2EHelperProcess", "--"}, args...)
}

func TestNativeE2EHelperProcess(t *testing.T) {
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	args := os.Args[separator+1:]
	switch args[0] {
	case "desktop":
		time.Sleep(30 * time.Second)
	case "crash":
		process, _ := os.FindProcess(os.Getpid())
		_ = process.Kill()
		select {}
	case "probe":
		if len(args) < 3 || args[2] != "pass" {
			os.Exit(7)
		}
		_, _ = os.Stdout.WriteString(args[1] + "=serving")
	case "route":
		if len(args) < 3 {
			os.Exit(8)
		}
		file, err := os.OpenFile(args[2], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(9)
		}
		_, _ = file.WriteString(args[1] + "\n")
		_ = file.Close()
		_, _ = os.Stdout.WriteString(`{"schema_version":1,"requested_route_id":"` + args[1] + `","observed_route_id":"` + args[1] + `","status":"passed","detail":"native route observed"}`)
	case "route-fail":
		os.Exit(10)
	case "route-empty":
		os.Exit(0)
	case "route-mismatch":
		_, _ = os.Stdout.WriteString(`{"schema_version":1,"requested_route_id":"` + args[1] + `","observed_route_id":"view.settings","status":"passed"}`)
	default:
		os.Exit(11)
	}
	os.Exit(0)
}

func TestResolveArtifactExecutableRejectsWrongHostArtifact(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	_, err := ResolveArtifactExecutable("windows", filepath.Join(t.TempDir(), "not-an-exe"))
	if err == nil {
		t.Fatal("无效 Windows 制品必须被拒绝")
	}
}
