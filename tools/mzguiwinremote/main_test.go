package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectAcceptsPassingEvidenceAndRejectsFailure(t *testing.T) {
	dir := t.TempDir()
	passPath := filepath.Join(dir, "pass.json")
	failPath := filepath.Join(dir, "fail.json")
	if err := os.WriteFile(passPath, []byte(validEvidenceJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failPath, []byte(failedEvidenceJSON()), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"inspect", "--from", passPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("通过证据应返回 0: code=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"allows_local_automated": true`)) {
		t.Fatalf("应报告允许更新 windows-x64: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"inspect", "--from", failPath}, &stdout, &stderr); code != 3 {
		t.Fatalf("失败证据应返回 3: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
}

func TestCollectRequiresConnectionFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"collect"}, &stdout, &stderr); code != 2 {
		t.Fatalf("缺少参数应返回 2: code=%d", code)
	}
}

func validEvidenceJSON() string {
	return `{"schema_version":1,"platform":"windows-x64","goos":"windows","goarch":"amd64","hostname":"WIN11-DEV","commit":"abc123def456","started_at":"2026-08-16T07:48:00Z","finished_at":"2026-08-16T07:55:00Z","status":"pass","steps":[{"name":"identify","status":"pass","detail":"windows/amd64"},{"name":"deviceprobe","status":"pass","detail":"ok"},{"name":"framework-go-test","status":"pass","detail":"ok"},{"name":"desktop-build","status":"pass","detail":"mz-gui-desktop.exe"}]}`
}

func failedEvidenceJSON() string {
	return `{"schema_version":1,"platform":"windows-x64","goos":"windows","goarch":"amd64","hostname":"WIN11-DEV","commit":"abc123def456","started_at":"2026-08-16T07:48:00Z","finished_at":"2026-08-16T07:55:00Z","status":"fail","steps":[{"name":"identify","status":"pass","detail":"windows/amd64"},{"name":"deviceprobe","status":"fail","detail":"boom"},{"name":"framework-go-test","status":"pass","detail":"ok"},{"name":"desktop-build","status":"pass","detail":"mz-gui-desktop.exe"}]}`
}
