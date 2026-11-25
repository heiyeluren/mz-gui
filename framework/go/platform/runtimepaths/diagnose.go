package runtimepaths

import (
	"os"
)

const (
	FindingOK         = "ok"
	FindingEmpty      = "empty"
	FindingMissing    = "missing"
	FindingSymlink    = "symlink"
	FindingNotDir     = "not_directory"
	FindingUnreadable = "unreadable"
)

type Finding struct {
	Path string
	Code string
}

func Diagnose(paths Paths) []Finding {
	findings := make([]Finding, 0, len(paths.Directories()))
	for _, directory := range paths.Directories() {
		if directory == "" {
			findings = append(findings, Finding{Code: FindingEmpty})
			continue
		}
		info, err := os.Lstat(directory)
		switch {
		case err == nil && isLinkLike(info):
			findings = append(findings, Finding{Path: directory, Code: FindingSymlink})
		case err == nil && !info.IsDir():
			findings = append(findings, Finding{Path: directory, Code: FindingNotDir})
		case err == nil:
			findings = append(findings, Finding{Path: directory, Code: FindingOK})
		case os.IsNotExist(err):
			findings = append(findings, Finding{Path: directory, Code: FindingMissing})
		default:
			findings = append(findings, Finding{Path: directory, Code: FindingUnreadable})
		}
	}
	return findings
}

func (finding Finding) Unsafe() bool {
	return finding.Code != FindingOK && finding.Code != FindingMissing
}

func Unsafe(findings []Finding) bool {
	for _, finding := range findings {
		if finding.Unsafe() {
			return true
		}
	}
	return false
}
