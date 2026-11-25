package main

import (
	"errors"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/security/allowlist"
)

func TestValidateShowcaseShellEntriesAcceptsDesktopProcessAndScheme(t *testing.T) {
	if err := validateShowcaseShellEntries(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateShowcaseShellEntriesUsesAllowlistDenial(t *testing.T) {
	if err := allowlist.CheckCommand("../desktop"); !errors.Is(err, allowlist.ErrDenied) {
		t.Fatalf("非法命令必须拒绝: %v", err)
	}
	if err := allowlist.CheckDeepLink("https://evil.example/x", []string{"mzgui-showcase"}); !errors.Is(err, allowlist.ErrDenied) {
		t.Fatalf("非登记 scheme 必须拒绝: %v", err)
	}
}
