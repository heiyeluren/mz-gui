package runtimepaths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnoseReportsMissingOKSymlinkAndOccupiedPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime")
	paths := build(testLayout(), filepath.Join, root, filepath.Join(root, "logs"), filepath.Join(root, "cache"), filepath.Join(root, "run"))
	findings := Diagnose(paths)
	if !hasCode(findings, FindingMissing) || Unsafe(findings) {
		t.Fatalf("缺失目录应记 missing 且不算占用: %#v", findings)
	}
	if err := Ensure(paths); err != nil {
		t.Fatal(err)
	}
	if Unsafe(Diagnose(paths)) {
		t.Fatal("创建后诊断必须全部安全")
	}

	occupied := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(occupied, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	filePaths := build(testLayout(), filepath.Join, occupied, filepath.Join(occupied, "logs"), filepath.Join(occupied, "cache"), filepath.Join(occupied, "run"))
	if !hasCode(Diagnose(filePaths), FindingNotDir) || !Unsafe(Diagnose(filePaths)) {
		t.Fatal("文件占用必须记 not_directory")
	}

	realRoot := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	createLinkLike(t, realRoot, linked)
	linkedPaths := build(testLayout(), filepath.Join, linked, filepath.Join(linked, "logs"), filepath.Join(linked, "cache"), filepath.Join(linked, "run"))
	if !hasCode(Diagnose(linkedPaths), FindingSymlink) {
		t.Fatal("符号链接必须记 symlink")
	}
}

func TestDiagnoseMarksEmptyPathUnsafe(t *testing.T) {
	findings := Diagnose(Paths{})
	if !hasCode(findings, FindingEmpty) || !Unsafe(findings) {
		t.Fatalf("空路径诊断错误: %#v", findings)
	}
}

func hasCode(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
