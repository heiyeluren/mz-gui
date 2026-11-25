// Package plugin 定义 Framework 插件 SDK 的 manifest 与进程隔离契约。
package plugin

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/managedprocess"
)

var ErrInvalidManifest = errors.New("Framework 插件 manifest 无效")
var ErrInvalidSignature = errors.New("Framework 插件签名无效")
var ErrUnknownTrustRoot = errors.New("Framework 插件信任根未知")

var (
	idPattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9-]*)+$`)
	namePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	schemaRefPattern = regexp.MustCompile(`^schema://[a-z][a-z0-9.-]*/[A-Za-z0-9._:-]{1,128}$`)
	digestPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	signaturePattern = regexp.MustCompile(`^[a-f0-9]{128}$`)
	envPattern       = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

//go:embed manifest-v1.schema.json
var builtinSchema []byte

type Manifest struct {
	SchemaRef             string          `json:"$schema"`
	SchemaVersion         int             `json:"schema_version"`
	PluginID              string          `json:"plugin_id"`
	DisplayName           string          `json:"display_name"`
	Publisher             string          `json:"publisher"`
	UpdateChannel         string          `json:"update_channel"`
	Process               Process         `json:"process"`
	RequestedCapabilities []string        `json:"requested_capabilities"`
	ResourceScopes        []ResourceScope `json:"resource_scopes"`
	Platforms             []Platform      `json:"platforms"`
	Signature             Signature       `json:"signature"`
	Isolation             Isolation       `json:"isolation"`
	Limits                Limits          `json:"limits"`
	ShutdownTimeout       time.Duration   `json:"-"`
}

type Process struct {
	ProtocolMajor     uint32 `json:"protocol_major"`
	ProtocolMinor     uint32 `json:"protocol_minor"`
	ExecutableID      string `json:"executable_id"`
	BuildID           string `json:"build_id"`
	ArgumentsSchema   string `json:"arguments_schema"`
	HealthCheck       string `json:"health_check"`
	ShutdownTimeoutMS int64  `json:"shutdown_timeout_ms"`
}

type ResourceScope struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type Signature struct {
	KeyID     string `json:"key_id"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

type Isolation struct {
	WorkdirMode  string   `json:"workdir_mode"`
	EnvAllowlist []string `json:"env_allowlist"`
	Stdio        string   `json:"stdio"`
	Network      string   `json:"network"`
}

type Limits struct {
	MaxMemoryMB      int   `json:"max_memory_mb"`
	MaxMessageBytes  int64 `json:"max_message_bytes"`
	StartupTimeoutMS int64 `json:"startup_timeout_ms"`
}

type LaunchOptions struct {
	ExecutablePath string
	WorkDir        string
	Environment    map[string]string
}

type SigningPayloadInput struct {
	PluginID     string
	ExecutableID string
	SHA256       string
}

func BuiltinSchemaJSON() []byte {
	return append([]byte(nil), builtinSchema...)
}

func DecodeManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Manifest{}, ErrInvalidManifest
	}
	if err := manifest.validate(); err != nil {
		return Manifest{}, err
	}
	manifest.ShutdownTimeout = time.Duration(manifest.Process.ShutdownTimeoutMS) * time.Millisecond
	return manifest, nil
}

func (manifest Manifest) ManagedProcessSpec(options LaunchOptions) (managedprocess.Spec, error) {
	if strings.TrimSpace(options.ExecutablePath) == "" || !filepath.IsAbs(options.ExecutablePath) ||
		strings.TrimSpace(options.WorkDir) == "" || !filepath.IsAbs(options.WorkDir) {
		return managedprocess.Spec{}, ErrInvalidManifest
	}
	allowedEnv := make(map[string]struct{}, len(manifest.Isolation.EnvAllowlist))
	for _, name := range manifest.Isolation.EnvAllowlist {
		allowedEnv[name] = struct{}{}
	}
	environment := make(map[string]string, len(allowedEnv))
	for key, value := range options.Environment {
		if _, ok := allowedEnv[key]; ok {
			environment[key] = value
		}
	}
	return managedprocess.Spec{
		ID:          "plugin." + manifest.PluginID,
		Executable:  options.ExecutablePath,
		Directory:   options.WorkDir,
		Environment: environment,
		Restart: managedprocess.RestartConfig{
			Policy:         managedprocess.RestartOnFailure,
			InitialBackoff: time.Second,
			MaxBackoff:     30 * time.Second,
			Multiplier:     2,
			Jitter:         0.2,
			Window:         time.Minute,
			MaxRestarts:    3,
			StableAfter:    2 * time.Minute,
		},
	}, nil
}

func (manifest Manifest) VerifyArtifactSignature(artifact []byte, trustRoots map[string]ed25519.PublicKey) error {
	if ArtifactSHA256(artifact) != manifest.Signature.SHA256 {
		return ErrInvalidSignature
	}
	publicKey, ok := trustRoots[manifest.Signature.KeyID]
	if !ok {
		return ErrUnknownTrustRoot
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return ErrInvalidSignature
	}
	signature, err := hex.DecodeString(manifest.Signature.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrInvalidSignature
	}
	payload := SigningPayload(SigningPayloadInput{
		PluginID:     manifest.PluginID,
		ExecutableID: manifest.Process.ExecutableID,
		SHA256:       manifest.Signature.SHA256,
	})
	if !ed25519.Verify(publicKey, payload, signature) {
		return ErrInvalidSignature
	}
	return nil
}

func ArtifactSHA256(artifact []byte) string {
	sum := sha256.Sum256(artifact)
	return hex.EncodeToString(sum[:])
}

func SigningPayload(input SigningPayloadInput) []byte {
	return []byte("mz-gui-plugin-v1\nplugin_id:" + input.PluginID + "\nexecutable_id:" + input.ExecutableID + "\nsha256:" + input.SHA256)
}

func (manifest Manifest) validate() error {
	if manifest.SchemaRef != "./manifest-v1.schema.json" || manifest.SchemaVersion != 1 ||
		!idPattern.MatchString(manifest.PluginID) || strings.TrimSpace(manifest.DisplayName) == "" ||
		len(manifest.DisplayName) > 80 || !namePattern.MatchString(manifest.Publisher) ||
		!namePattern.MatchString(manifest.UpdateChannel) || len(manifest.RequestedCapabilities) == 0 ||
		len(manifest.ResourceScopes) == 0 || len(manifest.Platforms) == 0 {
		return ErrInvalidManifest
	}
	if err := manifest.Process.validate(); err != nil {
		return err
	}
	if err := manifest.Signature.validate(); err != nil {
		return err
	}
	if err := manifest.Isolation.validate(); err != nil {
		return err
	}
	if err := manifest.Limits.validate(); err != nil {
		return err
	}
	if hasDuplicates(manifest.RequestedCapabilities) {
		return ErrInvalidManifest
	}
	for _, capability := range manifest.RequestedCapabilities {
		if !idPattern.MatchString(capability) {
			return ErrInvalidManifest
		}
	}
	for _, scope := range manifest.ResourceScopes {
		if !namePattern.MatchString(scope.Kind) || strings.TrimSpace(scope.Value) == "" || len(scope.Value) > 256 {
			return ErrInvalidManifest
		}
	}
	seenPlatforms := make(map[string]struct{}, len(manifest.Platforms))
	for _, platform := range manifest.Platforms {
		if !validOS(platform.OS) || !validArch(platform.Arch) {
			return ErrInvalidManifest
		}
		key := platform.OS + "/" + platform.Arch
		if _, exists := seenPlatforms[key]; exists {
			return ErrInvalidManifest
		}
		seenPlatforms[key] = struct{}{}
	}
	return nil
}

func (process Process) validate() error {
	if process.ProtocolMajor == 0 || !namePattern.MatchString(process.ExecutableID) ||
		strings.ContainsAny(process.ExecutableID, `/\`) || !namePattern.MatchString(process.BuildID) ||
		!schemaRefPattern.MatchString(process.ArgumentsSchema) || !namePattern.MatchString(process.HealthCheck) ||
		process.ShutdownTimeoutMS < 1000 || process.ShutdownTimeoutMS > 30000 {
		return ErrInvalidManifest
	}
	return nil
}

func (signature Signature) validate() error {
	if !namePattern.MatchString(signature.KeyID) || !digestPattern.MatchString(signature.SHA256) || !signaturePattern.MatchString(signature.Signature) {
		return ErrInvalidManifest
	}
	return nil
}

func (isolation Isolation) validate() error {
	if isolation.WorkdirMode != "private" || isolation.Stdio != "framed-json" || isolation.Network != "declared-only" ||
		len(isolation.EnvAllowlist) == 0 || hasDuplicates(isolation.EnvAllowlist) {
		return ErrInvalidManifest
	}
	for _, name := range isolation.EnvAllowlist {
		if !envPattern.MatchString(name) {
			return ErrInvalidManifest
		}
	}
	sort.Strings(isolation.EnvAllowlist)
	return nil
}

func (limits Limits) validate() error {
	if limits.MaxMemoryMB < 32 || limits.MaxMemoryMB > 4096 || limits.MaxMessageBytes < 1024 ||
		limits.MaxMessageBytes > 8<<20 || limits.StartupTimeoutMS < 500 || limits.StartupTimeoutMS > 60000 {
		return ErrInvalidManifest
	}
	return nil
}

func hasDuplicates(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != value || value == "" {
			return true
		}
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func validOS(value string) bool {
	return value == "darwin" || value == "windows" || value == "linux"
}

func validArch(value string) bool {
	return value == "amd64" || value == "arm64" || value == "universal"
}
