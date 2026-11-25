//go:build darwin

package startup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinRegistrationUsesInjectedProductPolicy(t *testing.T) {
	home := t.TempDir()
	executable := filepath.Join(home, "app", "sample-worker")
	stdout := filepath.Join(home, "logs", "sample.stdout.log")
	stderr := filepath.Join(home, "logs", "sample.stderr.log")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("worker"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &startupRecordingRunner{}
	registration := NewDarwinRegistration(DarwinRegistrationOptions{
		Label: "com.example.sample-worker", Home: home, Executable: executable,
		StdoutPath: stdout, StderrPath: stderr, Runner: runner, UID: 501,
	})
	if err := registration.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	status, err := registration.Status(context.Background())
	if err != nil || !status.Enabled || status.Drifted || status.Executable != executable {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	if !strings.Contains(status.Location, "com.example.sample-worker.plist") {
		t.Fatalf("location=%q", status.Location)
	}
	info, err := os.Stat(status.Location)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
	if err := registration.SetEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if !runner.contains("bootout gui/501/com.example.sample-worker") {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func TestDarwinRegistrationRetriesBootstrapAfterBootout(t *testing.T) {
	home := t.TempDir()
	executable := filepath.Join(home, "sample-worker")
	if err := os.WriteFile(executable, []byte("worker"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &startupRecordingRunner{failBootstrapOnce: true}
	registration := NewDarwinRegistration(DarwinRegistrationOptions{
		Label: "com.example.sample-worker", Home: home, Executable: executable,
		StdoutPath: filepath.Join(home, "stdout.log"), StderrPath: filepath.Join(home, "stderr.log"), Runner: runner, UID: 501,
	})
	if err := registration.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if runner.bootstrapCalls != 2 || !runner.contains("bootout gui/501/com.example.sample-worker") {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func TestDarwinRegistrationWaitsForBootoutToSettle(t *testing.T) {
	home := t.TempDir()
	executable := filepath.Join(home, "sample-worker")
	if err := os.WriteFile(executable, []byte("worker"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &startupRecordingRunner{failBootstrapCount: 2}
	registration := NewDarwinRegistration(DarwinRegistrationOptions{
		Label: "com.example.sample-worker", Home: home, Executable: executable,
		StdoutPath: filepath.Join(home, "stdout.log"), StderrPath: filepath.Join(home, "stderr.log"), Runner: runner, UID: 501,
	})
	if err := registration.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if runner.bootstrapCalls != 3 || !runner.contains("bootout gui/501/com.example.sample-worker") {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func TestDarwinRegistrationMigratesOwnedLegacyLabelsBeforeBootstrap(t *testing.T) {
	home := t.TempDir()
	executable := filepath.Join(home, "sample-worker")
	if err := os.WriteFile(executable, []byte("worker"), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyLabel := "com.example.legacy-worker"
	legacyPlist := filepath.Join(home, "Library", "LaunchAgents", legacyLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(legacyPlist), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPlist, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &startupRecordingRunner{}
	registration := NewDarwinRegistration(DarwinRegistrationOptions{
		Label: "com.example.sample-worker", LegacyLabels: []string{legacyLabel},
		Home: home, Executable: executable, StdoutPath: filepath.Join(home, "stdout.log"),
		StderrPath: filepath.Join(home, "stderr.log"), Runner: runner, UID: 501,
	})

	if err := registration.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyPlist); !os.IsNotExist(err) {
		t.Fatalf("旧 LaunchAgent 应被移除: %v", err)
	}
	legacyBootout := runner.indexOf("bootout gui/501/" + legacyLabel)
	currentBootstrap := runner.indexOf("bootstrap gui/501")
	if legacyBootout < 0 || currentBootstrap < 0 || legacyBootout >= currentBootstrap {
		t.Fatalf("旧注册必须先退出再加载当前注册: calls=%v", runner.calls)
	}
}

func TestDarwinRegistrationRejectsInvalidLegacyLabel(t *testing.T) {
	home := t.TempDir()
	executable := filepath.Join(home, "sample-worker")
	if err := os.WriteFile(executable, []byte("worker"), 0o700); err != nil {
		t.Fatal(err)
	}
	registration := NewDarwinRegistration(DarwinRegistrationOptions{
		Label: "com.example.sample-worker", LegacyLabels: []string{"../other-user"},
		Home: home, Executable: executable, StdoutPath: filepath.Join(home, "stdout.log"),
		StderrPath: filepath.Join(home, "stderr.log"), Runner: &startupRecordingRunner{}, UID: 501,
	})
	if err := registration.SetEnabled(context.Background(), true); err == nil || !strings.Contains(err.Error(), "旧 LaunchAgent label 无效") {
		t.Fatalf("err=%v", err)
	}
}

type startupRecordingRunner struct {
	calls              [][]string
	bootstrapCalls     int
	failBootstrapOnce  bool
	failBootstrapCount int
}

func (runner *startupRecordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	runner.calls = append(runner.calls, call)
	if name == "launchctl" && len(args) > 0 && args[0] == "bootstrap" {
		runner.bootstrapCalls++
		if (runner.failBootstrapOnce && runner.bootstrapCalls == 1) || runner.bootstrapCalls <= runner.failBootstrapCount {
			return nil, errors.New("bootstrap failed")
		}
	}
	return nil, nil
}

func (runner *startupRecordingRunner) contains(value string) bool {
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), value) {
			return true
		}
	}
	return false
}

func (runner *startupRecordingRunner) indexOf(value string) int {
	for index, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), value) {
			return index
		}
	}
	return -1
}
