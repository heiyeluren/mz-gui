package toolruntime

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
)

func TestResolveAcceptsPinnedAdjacentRuntime(t *testing.T) {
	host, runtimeDirectory, manifestDigest := createFixture(t, "amd64", "amd64", 0x8664)
	resolved, err := Resolve(Spec{
		HostExecutable: host, RuntimeID: "git-bash", Version: "2.55.0.4",
		Arch: "amd64", ManifestSHA256: manifestDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Directory != runtimeDirectory || resolved.Executable != filepath.Join(runtimeDirectory, "usr", "bin", "bash.exe") {
		t.Fatalf("resolved=%#v", resolved)
	}
}

func TestResolveAcceptsTrustedEmulatedEntrypoint(t *testing.T) {
	host, _, digest := createFixture(t, "arm64", "amd64", 0x8664)
	if _, err := Resolve(Spec{HostExecutable: host, RuntimeID: "git-bash", Version: "2.55.0.4", Arch: "arm64", ManifestSHA256: digest}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRejectsUntrustedRuntime(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, host, runtimeDirectory string, spec *Spec)
	}{
		{name: "manifest digest", mutate: func(_ *testing.T, _, _ string, spec *Spec) { spec.ManifestSHA256 = strings.Repeat("0", 64) }},
		{name: "modified file", mutate: func(t *testing.T, _, root string, _ *Spec) {
			if err := os.WriteFile(filepath.Join(root, "usr", "bin", "tool.exe"), []byte("changed"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unexpected file", mutate: func(t *testing.T, _, root string, _ *Spec) {
			if err := os.WriteFile(filepath.Join(root, "usr", "bin", "injected.exe"), []byte("bad"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", mutate: func(t *testing.T, _, root string, _ *Spec) {
			testkit.RequireSymlink(t)
			if err := os.Symlink(filepath.Join(root, "usr", "bin", "tool.exe"), filepath.Join(root, "usr", "bin", "linked.exe")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "architecture", mutate: func(t *testing.T, _, root string, _ *Spec) {
			writePE(t, filepath.Join(root, "usr", "bin", "bash.exe"), 0xAA64)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host, runtimeDirectory, digest := createFixture(t, "amd64", "amd64", 0x8664)
			spec := Spec{HostExecutable: host, RuntimeID: "git-bash", Version: "2.55.0.4", Arch: "amd64", ManifestSHA256: digest}
			test.mutate(t, host, runtimeDirectory, &spec)
			if _, err := Resolve(spec); err == nil {
				t.Fatal("不可信工具运行时必须拒绝")
			}
		})
	}
}

func TestResolveRejectsInvalidBoundary(t *testing.T) {
	host, _, digest := createFixture(t, "amd64", "amd64", 0x8664)
	for _, spec := range []Spec{
		{HostExecutable: "relative.exe", RuntimeID: "git-bash", Version: "2.55.0.4", Arch: "amd64", ManifestSHA256: digest},
		{HostExecutable: host, RuntimeID: "../git-bash", Version: "2.55.0.4", Arch: "amd64", ManifestSHA256: digest},
		{HostExecutable: host, RuntimeID: "git-bash", Version: "../2.55", Arch: "amd64", ManifestSHA256: digest},
		{HostExecutable: host, RuntimeID: "git-bash", Version: "2.55.0.4", Arch: "386", ManifestSHA256: digest},
		{HostExecutable: host, RuntimeID: "git-bash", Version: "2.55.0.4", Arch: "amd64", ManifestSHA256: "bad"},
	} {
		if _, err := Resolve(spec); err == nil {
			t.Fatalf("无效边界必须拒绝: %#v", spec)
		}
	}
}

func createFixture(t *testing.T, arch, entrypointArch string, machine uint16) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	host := filepath.Join(root, "mz-gui-agentd.exe")
	if err := os.WriteFile(host, []byte("agentd"), 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeDirectory := filepath.Join(root, "tool-runtimes", "git-bash", "2.55.0.4")
	protectedRoot := filepath.Join(runtimeDirectory, "usr", "bin")
	if err := os.MkdirAll(protectedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	bashPath := filepath.Join(protectedRoot, "bash.exe")
	writePE(t, bashPath, machine)
	toolPath := filepath.Join(protectedRoot, "tool.exe")
	if err := os.WriteFile(toolPath, []byte("tool"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: 1, RuntimeID: "git-bash", Version: "2.55.0.4", Arch: arch,
		Entrypoint: "usr/bin/bash.exe", EntrypointArch: entrypointArch, ProtectedRoots: []string{"usr/bin"}, Files: []FileRecord{
			fileRecord(t, runtimeDirectory, "usr/bin/bash.exe"), fileRecord(t, runtimeDirectory, "usr/bin/tool.exe"),
		}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDirectory, ManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return host, runtimeDirectory, hex.EncodeToString(digest[:])
}

func fileRecord(t *testing.T, root, relative string) FileRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return FileRecord{Path: relative, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func writePE(t *testing.T, path string, machine uint16) {
	t.Helper()
	data := make([]byte, 96)
	data[0], data[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(data[0x3c:], 0x40)
	copy(data[0x40:], []byte{'P', 'E', 0, 0})
	binary.LittleEndian.PutUint16(data[0x44:], machine)
	if err := os.WriteFile(path, data, 0o700); err != nil {
		t.Fatal(err)
	}
}
