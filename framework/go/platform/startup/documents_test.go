package startup

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderLaunchAgentUsesInjectedIdentity(t *testing.T) {
	root := t.TempDir()
	spec := LaunchAgentSpec{
		Label:      "com.example.sample-worker",
		Executable: filepath.Join(root, "sample-worker"),
		StdoutPath: filepath.Join(root, "logs", "stdout.log"),
		StderrPath: filepath.Join(root, "logs", "stderr.log"),
	}
	data, err := RenderLaunchAgent(spec)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{spec.Label, spec.Executable, spec.StdoutPath, spec.StderrPath, "RunAtLoad", "KeepAlive"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("LaunchAgent 缺少 %q:\n%s", expected, text)
		}
	}
	if _, err := RenderLaunchAgent(LaunchAgentSpec{Label: " ", Executable: spec.Executable, StdoutPath: spec.StdoutPath, StderrPath: spec.StderrPath}); err == nil {
		t.Fatal("空 LaunchAgent label 必须拒绝")
	}
}

func TestWindowsTaskDocumentUsesInjectedLocationAndDetectsDrift(t *testing.T) {
	installed := `C:\\Old\\sample-worker.exe`
	current := `C:\\Program Files\\Sample\\sample-worker.exe`
	location := `\\Example\\SampleWorker`
	data, err := RenderWindowsTask(WindowsTaskSpec{Executable: installed, UserSID: "S-1-5-21-1000"})
	if err != nil {
		t.Fatal(err)
	}
	status, err := ParseWindowsTaskStatus(data, current, location)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || !status.Drifted || status.Location != location || status.Executable != installed {
		t.Fatalf("status=%#v", status)
	}
	if _, err := RenderWindowsTask(WindowsTaskSpec{Executable: installed}); err == nil {
		t.Fatal("空 SID 必须拒绝")
	}
}

func TestParseWindowsTaskStatusRejectsInvalidDocuments(t *testing.T) {
	if _, err := ParseWindowsTaskStatus([]byte("broken"), `C:\\Sample\\worker.exe`, `\\Sample\\Worker`); err == nil {
		t.Fatal("损坏 XML 必须拒绝")
	}
	if _, err := ParseWindowsTaskStatus([]byte(`<Task><Actions><Exec/></Actions></Task>`), `C:\\Sample\\worker.exe`, `\\Sample\\Worker`); err == nil {
		t.Fatal("缺少程序必须拒绝")
	}
	if _, err := RenderWindowsTask(WindowsTaskSpec{Executable: "relative.exe", UserSID: "S-1-5-21-1"}); err == nil {
		t.Fatal("相对 Windows 程序路径必须拒绝")
	}
}
