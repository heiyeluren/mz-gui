package updater

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidManifest   = errors.New("更新清单无效")
	ErrUntrustedManifest = errors.New("更新清单签名密钥不受信任")
	ErrInvalidSignature  = errors.New("更新清单签名无效")
	ErrManifestPolicy    = errors.New("更新清单不符合产品策略")
	ErrManifestExpired   = errors.New("更新清单已过期")
	ErrArtifactNotFound  = errors.New("没有匹配当前平台的更新制品")
	ErrInvalidSigningKey = errors.New("更新清单签名密钥无效")
)

const maximumArtifactBytes int64 = 4 << 30

var (
	sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	namePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// Artifact 是一个已由发布清单绑定的平台安装制品。
type Artifact struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	Kind          string `json:"kind"`
	URL           string `json:"url"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	MinimumOS     string `json:"minimum_os,omitempty"`
	BaseSHA256    string `json:"base_sha256,omitempty"`
	SignatureType string `json:"signature_type,omitempty"`
	Component     string `json:"component,omitempty"`
}

// Payload 是离线发布密钥签名的稳定内容。
type Payload struct {
	SchemaVersion           int        `json:"schema_version"`
	Product                 string     `json:"product"`
	Channel                 string     `json:"channel"`
	Version                 string     `json:"version"`
	BuildID                 string     `json:"build_id,omitempty"`
	Commit                  string     `json:"commit,omitempty"`
	MinimumSupportedVersion string     `json:"minimum_supported_version,omitempty"`
	ForceUpdate             bool       `json:"force_update,omitempty"`
	ReleaseNotes            string     `json:"release_notes,omitempty"`
	PublishedAt             string     `json:"published_at"`
	ExpiresAt               string     `json:"expires_at"`
	Artifacts               []Artifact `json:"artifacts"`
}

// Envelope 将签名身份与签名内容分离，签名仅覆盖 Payload 的规范 JSON。
type Envelope struct {
	KeyID     string  `json:"key_id"`
	Signature string  `json:"signature"`
	Payload   Payload `json:"payload"`
}

type Manifest struct {
	KeyID         string
	Payload       Payload
	payloadDigest [sha256.Size]byte
}

type VerifyPolicy struct {
	Product string
	Channel string
	Keys    map[string]ed25519.PublicKey
	Now     func() time.Time
}

type Decision struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	ForceRequired   bool
	BelowMinimum    bool
}

// Sign 校验发布内容并生成稳定的 Ed25519 签名信封；私钥由调用方在离线发布环境提供。
func Sign(payload Payload, keyID string, privateKey ed25519.PrivateKey) ([]byte, error) {
	if !namePattern.MatchString(keyID) || len(privateKey) != ed25519.PrivateKeySize {
		return nil, ErrInvalidSigningKey
	}
	if err := validatePayload(payload); err != nil {
		return nil, err
	}
	canonicalPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	envelope := Envelope{
		KeyID:     keyID,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, canonicalPayload)),
		Payload:   payload,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	return data, nil
}

func ParseAndVerify(data []byte, policy VerifyPolicy) (Manifest, error) {
	if len(data) == 0 || len(data) > 1<<20 || !namePattern.MatchString(policy.Product) || !validChannel(policy.Channel) {
		return Manifest{}, ErrInvalidManifest
	}
	var envelope Envelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Manifest{}, fmt.Errorf("%w: 清单包含尾随内容", ErrInvalidManifest)
	}
	publicKey, ok := policy.Keys[envelope.KeyID]
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return Manifest{}, ErrUntrustedManifest
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Manifest{}, ErrInvalidSignature
	}
	canonicalPayload, err := json.Marshal(envelope.Payload)
	if err != nil || !ed25519.Verify(publicKey, canonicalPayload, signature) {
		return Manifest{}, ErrInvalidSignature
	}
	if err := validatePayload(envelope.Payload); err != nil {
		return Manifest{}, err
	}
	if envelope.Payload.Product != policy.Product || envelope.Payload.Channel != policy.Channel {
		return Manifest{}, ErrManifestPolicy
	}
	now := time.Now
	if policy.Now != nil {
		now = policy.Now
	}
	expiresAt, _ := time.Parse(time.RFC3339, envelope.Payload.ExpiresAt)
	if !now().UTC().Before(expiresAt) {
		return Manifest{}, ErrManifestExpired
	}
	return Manifest{KeyID: envelope.KeyID, Payload: envelope.Payload, payloadDigest: sha256.Sum256(canonicalPayload)}, nil
}

// Verified 确认该值由 ParseAndVerify 产生，且验签后的 Payload 未被调用方修改。
func (manifest Manifest) Verified() bool {
	canonicalPayload, err := json.Marshal(manifest.Payload)
	return err == nil && manifest.payloadDigest != [sha256.Size]byte{} && sha256.Sum256(canonicalPayload) == manifest.payloadDigest
}

func (manifest Manifest) SelectArtifact(goos, goarch string) (Artifact, error) {
	return manifest.SelectArtifactKind(goos, goarch, "installer")
}

// SelectArtifactKind 为桌面安装包之外的受信任平台制品提供同一套架构选择规则。
func (manifest Manifest) SelectArtifactKind(goos, goarch, kind string) (Artifact, error) {
	return manifest.selectArtifact(goos, goarch, kind, "")
}

// SelectProcessArtifact 选择同一签名清单中指定进程组件的完整包。
func (manifest Manifest) SelectProcessArtifact(goos, goarch, component string) (Artifact, error) {
	component = strings.TrimSpace(component)
	if component == "" || !validProcessComponent(component) {
		return Artifact{}, ErrArtifactNotFound
	}
	return manifest.selectArtifact(goos, goarch, "full", component)
}

func (manifest Manifest) selectArtifact(goos, goarch, kind, component string) (Artifact, error) {
	goos = strings.ToLower(strings.TrimSpace(goos))
	goarch = strings.ToLower(strings.TrimSpace(goarch))
	kind = strings.TrimSpace(kind)
	component = strings.TrimSpace(component)
	for _, artifact := range manifest.Payload.Artifacts {
		if artifact.OS == goos && artifact.Arch == goarch && artifact.Kind == kind && artifact.Component == component {
			return artifact, nil
		}
	}
	if goos == "darwin" {
		for _, artifact := range manifest.Payload.Artifacts {
			if artifact.OS == goos && artifact.Arch == "universal" && artifact.Kind == kind && artifact.Component == component {
				return artifact, nil
			}
		}
	}
	return Artifact{}, ErrArtifactNotFound
}

func (manifest Manifest) Evaluate(currentVersion string) (Decision, error) {
	comparison, err := CompareVersions(currentVersion, manifest.Payload.Version)
	if err != nil {
		return Decision{}, err
	}
	decision := Decision{
		CurrentVersion: currentVersion, LatestVersion: manifest.Payload.Version,
		UpdateAvailable: comparison < 0,
	}
	if manifest.Payload.MinimumSupportedVersion != "" {
		minimumComparison, compareErr := CompareVersions(currentVersion, manifest.Payload.MinimumSupportedVersion)
		if compareErr != nil {
			return Decision{}, compareErr
		}
		decision.BelowMinimum = minimumComparison < 0
	}
	decision.ForceRequired = decision.BelowMinimum || (decision.UpdateAvailable && manifest.Payload.ForceUpdate)
	return decision, nil
}

func validatePayload(payload Payload) error {
	if payload.SchemaVersion != 1 || !namePattern.MatchString(payload.Product) || !validChannel(payload.Channel) {
		return ErrInvalidManifest
	}
	if _, err := parseVersion(payload.Version); err != nil {
		return fmt.Errorf("%w: 发布版本非法", ErrInvalidManifest)
	}
	if payload.MinimumSupportedVersion != "" {
		if _, err := parseVersion(payload.MinimumSupportedVersion); err != nil {
			return fmt.Errorf("%w: 最低支持版本非法", ErrInvalidManifest)
		}
		if comparison, _ := CompareVersions(payload.MinimumSupportedVersion, payload.Version); comparison > 0 {
			return fmt.Errorf("%w: 最低支持版本高于发布版本", ErrInvalidManifest)
		}
	}
	publishedAt, err := time.Parse(time.RFC3339, payload.PublishedAt)
	if err != nil {
		return fmt.Errorf("%w: 发布时间非法", ErrInvalidManifest)
	}
	expiresAt, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil || !expiresAt.After(publishedAt) {
		return fmt.Errorf("%w: 过期时间非法", ErrInvalidManifest)
	}
	if len(payload.ReleaseNotes) > 64<<10 || len(payload.Artifacts) == 0 || len(payload.Artifacts) > 32 {
		return ErrInvalidManifest
	}
	seen := make(map[string]struct{}, len(payload.Artifacts))
	for _, artifact := range payload.Artifacts {
		if err := validateArtifact(artifact); err != nil {
			return err
		}
		key := artifact.OS + "/" + artifact.Arch + "/" + artifact.Kind + "/" + artifact.Component
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: 存在重复的平台制品", ErrInvalidManifest)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateArtifact(artifact Artifact) error {
	if artifact.OS != "darwin" && artifact.OS != "windows" && artifact.OS != "linux" {
		return fmt.Errorf("%w: 制品操作系统非法", ErrInvalidManifest)
	}
	if artifact.Arch != "amd64" && artifact.Arch != "arm64" && !(artifact.OS == "darwin" && artifact.Arch == "universal") {
		return fmt.Errorf("%w: 制品架构非法", ErrInvalidManifest)
	}
	if !validProcessComponent(artifact.Component) {
		return fmt.Errorf("%w: 制品进程组件非法", ErrInvalidManifest)
	}
	if artifact.Component != "" && artifact.Kind != "full" {
		return fmt.Errorf("%w: 进程组件制品必须是完整包", ErrInvalidManifest)
	}
	if artifact.Kind != "installer" && artifact.Kind != "full" && artifact.Kind != "delta" {
		return fmt.Errorf("%w: 制品类型非法", ErrInvalidManifest)
	}
	if artifact.Size < 1 || artifact.Size > maximumArtifactBytes || !sha256Pattern.MatchString(strings.ToLower(artifact.SHA256)) {
		return fmt.Errorf("%w: 制品大小或摘要非法", ErrInvalidManifest)
	}
	if artifact.Kind == "delta" && !sha256Pattern.MatchString(strings.ToLower(artifact.BaseSHA256)) {
		return fmt.Errorf("%w: 差分制品必须声明基础摘要", ErrInvalidManifest)
	}
	if artifact.BaseSHA256 != "" && !sha256Pattern.MatchString(strings.ToLower(artifact.BaseSHA256)) {
		return fmt.Errorf("%w: 差分基础摘要非法", ErrInvalidManifest)
	}
	parsed, err := url.Parse(artifact.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%w: 制品地址必须是无凭据的 HTTPS URL", ErrInvalidManifest)
	}
	if !validInstallerSuffix(artifact.OS, parsed.Path, artifact.Kind) {
		return fmt.Errorf("%w: 安装制品扩展名与平台不匹配", ErrInvalidManifest)
	}
	return nil
}

func validInstallerSuffix(goos, value, kind string) bool {
	if kind != "installer" {
		return strings.HasSuffix(strings.ToLower(value), ".zip")
	}
	value = strings.ToLower(value)
	if goos == "darwin" {
		return strings.HasSuffix(value, ".dmg") || strings.HasSuffix(value, ".pkg")
	}
	return strings.HasSuffix(value, ".exe") || strings.HasSuffix(value, ".msix")
}

func validChannel(value string) bool {
	switch value {
	case "dev", "beta", "stable":
		return true
	default:
		return false
	}
}
