package runtimepaths

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWindowsLocalAppDataUsesEnvWhenSet(t *testing.T) {
	got, err := resolveWindowsLocalAppData(`C:\Users\tester\AppData\Local`, `C:\Users\tester`)
	if err != nil {
		t.Fatal(err)
	}
	if got != `C:\Users\tester\AppData\Local` {
		t.Fatalf("got %q", got)
	}
}

func TestResolveWindowsLocalAppDataFallsBackToUserProfile(t *testing.T) {
	got, err := resolveWindowsLocalAppData("", `C:\Users\tester`)
	if err != nil {
		t.Fatalf("仅有 USERPROFILE 时必须能解析: %v", err)
	}
	want := filepath.Join(`C:\Users\tester`, "AppData", "Local")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	paths, err := Resolve("windows", testLayout(), Inputs{LocalAppData: got})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(paths.Root, "AppData") {
		t.Fatalf("回退后的运行根无效: %q", paths.Root)
	}
}

func TestResolveWindowsLocalAppDataFatalWithoutProfile(t *testing.T) {
	if _, err := resolveWindowsLocalAppData("", ""); err == nil {
		t.Fatal("LOCALAPPDATA 与 USERPROFILE 都空时必须失败")
	}
}
