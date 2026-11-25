package updater

import (
	"errors"
	"testing"
)

func TestCheckProcessSetAcceptsMatchingDesktopAndSidecars(t *testing.T) {
	err := CheckProcessSet(ProcessSet{
		Desktop:  ProcessArtifact{Name: "desktop", Version: "1.2.3", BuildID: "build-1"},
		Sidecars: []ProcessArtifact{{Name: "agentd", Version: "1.2.3", BuildID: "build-1"}},
	})
	if err != nil {
		t.Fatalf("同版本进程集合应通过: %v", err)
	}
	if err := CheckProcessSet(ProcessSet{Desktop: ProcessArtifact{Name: "desktop", Version: "1.0.0"}}); err != nil {
		t.Fatalf("无 sidecar 也应通过: %v", err)
	}
}

func TestCheckProcessSetRejectsMismatchAndInvalidEntries(t *testing.T) {
	if err := CheckProcessSet(ProcessSet{
		Desktop:  ProcessArtifact{Name: "desktop", Version: "1.2.3"},
		Sidecars: []ProcessArtifact{{Name: "agentd", Version: "1.2.4"}},
	}); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("版本不一致必须拒绝: %v", err)
	}
	if err := CheckProcessSet(ProcessSet{
		Desktop:  ProcessArtifact{Name: "desktop", Version: "1.2.3", BuildID: "build-1"},
		Sidecars: []ProcessArtifact{{Name: "agentd", Version: "1.2.3", BuildID: "build-2"}},
	}); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("BuildID 不一致必须拒绝: %v", err)
	}
	if err := CheckProcessSet(ProcessSet{Desktop: ProcessArtifact{Name: "desktop", Version: "bad"}}); !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("非法版本必须拒绝: %v", err)
	}
	if err := CheckProcessSet(ProcessSet{
		Desktop:  ProcessArtifact{Name: "desktop", Version: "1.2.3"},
		Sidecars: []ProcessArtifact{{Name: "", Version: "1.2.3"}},
	}); !errors.Is(err, ErrInvalidProcessSet) {
		t.Fatalf("空 sidecar 名必须拒绝: %v", err)
	}
}

func TestRequireStartableProcessSetRejectsMixedOrIncompleteSet(t *testing.T) {
	ok := ProcessSet{
		Desktop:  ProcessArtifact{Name: ComponentDesktop, Version: "1.2.3", BuildID: "build-1"},
		Sidecars: []ProcessArtifact{{Name: ComponentAgentd, Version: "1.2.3", BuildID: "build-1"}},
	}
	if err := RequireStartableProcessSet(ok); err != nil {
		t.Fatalf("同版本 desktop+agentd 应允许启动: %v", err)
	}
	if err := RequireStartableProcessSet(ProcessSet{
		Desktop:  ProcessArtifact{Name: ComponentDesktop, Version: "1.2.3", BuildID: "build-1"},
		Sidecars: []ProcessArtifact{{Name: ComponentAgentd, Version: "1.2.4", BuildID: "build-1"}},
	}); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("混跑必须拒绝启动: %v", err)
	}
	if err := RequireStartableProcessSet(ProcessSet{Desktop: ProcessArtifact{Name: ComponentDesktop, Version: "1.2.3"}}); !errors.Is(err, ErrInvalidProcessSet) {
		t.Fatalf("缺少 agentd 必须拒绝启动: %v", err)
	}
}
