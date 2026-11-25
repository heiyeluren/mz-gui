package startup

import (
	"os"
	"path/filepath"
)

const (
	SidecarOK            = "ok"
	SidecarEmpty         = "empty"
	SidecarMissing       = "missing"
	SidecarSymlink       = "symlink"
	SidecarNotRegular    = "not_regular"
	SidecarNotExecutable = "not_executable"
	SidecarUnreadable    = "unreadable"
)

type SidecarFinding struct {
	Path string
	Code string
}

func DiagnoseSidecar(path string, requireExecutable bool) SidecarFinding {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		return SidecarFinding{Code: SidecarEmpty}
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil && isLinkLike(info):
		return SidecarFinding{Path: path, Code: SidecarSymlink}
	case err == nil && !info.Mode().IsRegular():
		return SidecarFinding{Path: path, Code: SidecarNotRegular}
	case err == nil && requireExecutable && !fileLooksExecutable(path, info.Mode()):
		return SidecarFinding{Path: path, Code: SidecarNotExecutable}
	case err == nil:
		return SidecarFinding{Path: path, Code: SidecarOK}
	case os.IsNotExist(err):
		return SidecarFinding{Path: path, Code: SidecarMissing}
	default:
		return SidecarFinding{Path: path, Code: SidecarUnreadable}
	}
}

func (finding SidecarFinding) Unsafe() bool {
	return finding.Code != SidecarOK && finding.Code != SidecarMissing
}
