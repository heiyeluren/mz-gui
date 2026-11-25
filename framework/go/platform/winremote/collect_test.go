package winremote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeTransport struct {
	runErr  error
	pullErr error
	payload []byte
	ran     [][]string
	pulled  [][2]string
}

func (transport *fakeTransport) Run(_ context.Context, argv []string) (CommandResult, error) {
	transport.ran = append(transport.ran, append([]string{}, argv...))
	if transport.runErr != nil {
		return CommandResult{Code: 1, Stderr: transport.runErr.Error()}, transport.runErr
	}
	return CommandResult{Code: 0, Stdout: "ok"}, nil
}

func (transport *fakeTransport) Pull(_ context.Context, remotePath, localPath string) error {
	transport.pulled = append(transport.pulled, [2]string{remotePath, localPath})
	if transport.pullErr != nil {
		return transport.pullErr
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(localPath, transport.payload, 0o600)
}

func TestCollectWritesValidatedWindowsEvidence(t *testing.T) {
	output := filepath.Join(t.TempDir(), "windows-x64-evidence.json")
	transport := &fakeTransport{payload: []byte(validEvidenceJSON())}
	evidence, err := Collect(context.Background(), transport, Options{
		RemoteRepo:   `C:\src\wails-mz-gui`,
		RemoteOutput: `C:\src\wails-mz-gui\.mzgui-windows-evidence`,
		LocalOutput:  output,
		Commit:       "abc123def456",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.AllowsLocalAutomated() {
		t.Fatal("采集到的通过证据应允许更新 windows-x64")
	}
	if len(transport.ran) != 1 || !strings.Contains(strings.Join(transport.ran[0], " "), "Invoke-MzGuiWindowsDevicePack.ps1") {
		t.Fatalf("未调度 Windows 测试包: %#v", transport.ran)
	}
	saved, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != validEvidenceJSON() {
		t.Fatalf("本地证据未按原文落盘: %s", saved)
	}
}

func TestCollectRejectsCommitMismatchAndFailedRemote(t *testing.T) {
	output := filepath.Join(t.TempDir(), "windows-x64-evidence.json")
	transport := &fakeTransport{payload: []byte(validEvidenceJSON())}
	if _, err := Collect(context.Background(), transport, Options{
		RemoteRepo:   `C:\src\wails-mz-gui`,
		RemoteOutput: `C:\src\wails-mz-gui\.mzgui-windows-evidence`,
		LocalOutput:  output,
		Commit:       "ffffffffffffffff",
	}); !errors.Is(err, ErrCommitMismatch) {
		t.Fatalf("提交不一致应失败: %v", err)
	}

	failed := &fakeTransport{runErr: errors.New("ssh timeout")}
	if _, err := Collect(context.Background(), failed, Options{
		RemoteRepo:   `C:\src\wails-mz-gui`,
		RemoteOutput: `C:\src\wails-mz-gui\.mzgui-windows-evidence`,
		LocalOutput:  output,
	}); err == nil {
		t.Fatal("远端调度失败时必须返回错误")
	}
}
