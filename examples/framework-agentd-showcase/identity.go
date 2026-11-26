package main

import (
	"runtime"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/transport/bootstrap"
)

func showcaseIdentity() bootstrap.ProcessIdentity {
	return bootstrap.ProcessIdentity{
		ProcessRole: "agentd", ProcessName: "showcase-agentd", ProcessVersion: "0.0.4", BuildID: "build-v0.0.4",
		InstanceID: "showcase-agentd-1", OS: runtime.GOOS, Arch: runtime.GOARCH, ReleaseChannel: "dev", EndpointID: "showcase-agentd",
		Application: bootstrap.ApplicationIdentity{
			AppID: "com.mzgui.framework.agentdshowcase", FrameworkAPIVersion: "1.0.0", FrameworkCatalogRevision: 1,
			FrameworkCatalogSHA256: strings.Repeat("a", 64), AppManifestSHA256: strings.Repeat("b", 64), FrameworkSelectionSHA256: strings.Repeat("c", 64),
		},
	}
}

func showcaseClientIdentity() bootstrap.ProcessIdentity {
	identity := showcaseIdentity()
	identity.ProcessRole = "desktop"
	identity.ProcessName = "showcase-desktop"
	identity.InstanceID = "showcase-desktop-1"
	return identity
}
