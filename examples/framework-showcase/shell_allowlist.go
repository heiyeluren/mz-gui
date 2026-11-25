package main

import (
	"github.com/heiyeluren/mz-gui/examples/framework-showcase/generated/appmeta"
	"github.com/heiyeluren/mz-gui/framework/go/security/allowlist"
)

func validateShowcaseShellEntries() error {
	if err := allowlist.CheckCommand(appmeta.DesktopProcess); err != nil {
		return err
	}
	if err := allowlist.CheckDeepLink(appmeta.ProtocolScheme+"://showcase/ready", []string{appmeta.ProtocolScheme}); err != nil {
		return err
	}
	if err := unlockShowcaseShellSession(); err != nil {
		return err
	}
	return probeShowcaseCrashDiagnostics()
}
