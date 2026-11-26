package main

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/security/filegrant"
	securitysession "github.com/heiyeluren/mz-gui/framework/go/security/session"
	"github.com/heiyeluren/mz-gui/framework/go/transport/bootstrap"
	bootstrapv1 "github.com/heiyeluren/mz-gui/gen/go/mzgui/framework/bootstrap/v1"
)

func probeShowcaseFileGrant(dir string) error {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	policy := filegrant.Policy{
		AllowedSubjects: []string{"desktop"},
		AllowedSources:  []string{"automation"},
		PersistentKinds: []filegrant.Kind{filegrant.KindDirectory},
		AccessByKind:    map[filegrant.Kind][]filegrant.Access{filegrant.KindDirectory: {filegrant.AccessReadWrite}},
	}
	resolved, err := policy.ValidateCreate(filegrant.CreateRequest{
		Subject: "desktop", Path: dir, Kind: filegrant.KindDirectory,
		Access: filegrant.AccessReadWrite, Source: "automation", Now: now,
	}, "showcase-file-grant-1")
	if err != nil {
		return err
	}
	grant := filegrant.Grant{Subject: "desktop", Path: resolved, Kind: filegrant.KindDirectory, Access: filegrant.AccessReadWrite, GrantedAt: now}
	if !grant.Active(now) {
		return fmt.Errorf("showcase FileGrant 未生效")
	}
	return nil
}

func probeShowcaseBootstrapHandshake(ctx context.Context) error {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	store, err := securitysession.NewSessionStore(securitysession.StoreOptions{TTL: 15 * time.Minute, MaxSessions: 8, Now: func() time.Time { return now }})
	if err != nil {
		return err
	}
	identity := bootstrap.ProcessIdentity{
		ProcessRole: "agentd", ProcessName: "showcase-agentd", ProcessVersion: "0.0.5", BuildID: "build-v0.0.5",
		InstanceID: "showcase-agentd-1", OS: runtime.GOOS, Arch: runtime.GOARCH, ReleaseChannel: "dev", EndpointID: "showcase-agentd",
		Application: bootstrap.ApplicationIdentity{
			AppID: "com.mzgui.framework.showcase", FrameworkAPIVersion: "1.0.0", FrameworkCatalogRevision: 1,
			FrameworkCatalogSHA256: strings.Repeat("a", 64), AppManifestSHA256: strings.Repeat("b", 64), FrameworkSelectionSHA256: strings.Repeat("c", 64),
		},
	}
	service, err := bootstrap.New(bootstrap.Config{
		ProtocolMajor: bootstrap.ProtocolMajor, ProtocolMinor: bootstrap.ProtocolMinor,
		Server: identity, Roles: map[string]bootstrap.RolePolicy{
			"desktop": {ProcessName: "showcase-desktop", Capabilities: []string{"system.health"}},
		},
		Sessions: store, Now: func() time.Time { return now }, Random: bytes.NewReader(bytes.Repeat([]byte{7}, 4096)),
		MaxClockSkew: 2 * time.Minute, NonceTTL: 5 * time.Minute, MaxNonces: 16,
	})
	if err != nil {
		return err
	}
	client := identity
	client.ProcessRole = "desktop"
	client.ProcessName = "showcase-desktop"
	client.InstanceID = "showcase-desktop-1"
	request := &bootstrapv1.BootstrapHandshakeRequest{
		Protocol: &bootstrapv1.ProtocolVersion{Major: bootstrap.ProtocolMajor, Minor: bootstrap.ProtocolMinor},
		Client:   client.Proto(), ClientNonce: []byte("0123456789abcdef0123456789abcdef"),
		ClientStartedAtUnixMs: now.Add(-time.Minute).UnixMilli(), RequestedAtUnixMs: now.UnixMilli(),
		RequestedCapabilities: []string{"system.health"},
	}
	response, err := service.Handshake(ctx, request)
	if err != nil {
		return err
	}
	if !response.GetAccepted() {
		return fmt.Errorf("showcase Bootstrap 握手被拒绝: %s", response.GetRejectionCode())
	}
	return bootstrap.VerifyResponse(request, response, identity, now)
}
