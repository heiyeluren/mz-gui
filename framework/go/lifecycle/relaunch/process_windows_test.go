//go:build windows

package relaunch

import (
	"encoding/json"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 读取真实子进程的 STARTUPINFO，防止重启把 GUI 首个窗口强制隐藏。
func TestDetachedGUIStartupDoesNotForceHiddenWindow(t *testing.T) {
	if path := os.Getenv("MZ_RELAUNCH_STARTUP_EVIDENCE"); path != "" {
		var info windows.StartupInfo
		windows.GetStartupInfo(&info)
		data, _ := json.Marshal(map[string]uint32{"flags": info.Flags, "show": uint32(info.ShowWindow)})
		if err := os.WriteFile(path, data, 0600); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "startup.json")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestDetachedGUIStartupDoesNotForceHiddenWindow$")
	command.Env = append(os.Environ(), "MZ_RELAUNCH_STARTUP_EVIDENCE="+path)
	detach(command)
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var info map[string]uint32
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	if info["flags"]&windows.STARTF_USESHOWWINDOW != 0 && info["show"] == windows.SW_HIDE {
		t.Fatalf("重启强制隐藏 GUI 窗口: %s", data)
	}
}
