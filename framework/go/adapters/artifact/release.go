package artifact

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	ErrReleaseManifest         = errors.New("制品发布目录无效")
	ErrReleaseUntrusted        = errors.New("制品发布目录不受信任")
	ErrReleaseSignature        = errors.New("制品发布目录签名无效")
	ErrReleasePolicy           = errors.New("制品发布目录不符合策略")
	ErrReleaseExpired          = errors.New("制品发布目录已过期")
	ErrReleaseNotFound         = errors.New("可信制品不存在")
	ErrReleaseRequest          = errors.New("可信制品请求无效")
	ErrReleaseArtifactMismatch = errors.New("本地制品与可信发布记录不一致")
)

var (
	releaseNamePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	releaseVersionPattern = regexp.MustCompile(`^[vV]?[0-9A-Za-z][0-9A-Za-z._+-]{0,79}$`)
	releaseDigestPattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

const defaultMaximumReleaseBytes int64 = 4 << 30

// ReleaseArtifact 把业务身份、平台、合同版本与制品摘要绑定在同一签名记录中。
type ReleaseArtifact struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Version         string `json:"version"`
	SHA256          string `json:"sha256"`
	Bytes           int64  `json:"bytes"`
	Format          string `json:"format"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	ContractVersion int    `json:"contract_version"`
}

type ReleasePayload struct {
	SchemaVersion int               `json:"schema_version"`
	Namespace     string            `json:"namespace"`
	Channel       string            `json:"channel"`
	GeneratedAt   string            `json:"generated_at"`
	ExpiresAt     string            `json:"expires_at"`
	Artifacts     []ReleaseArtifact `json:"artifacts"`
}

type ReleaseEnvelope struct {
	KeyID     string         `json:"key_id"`
	Signature string         `json:"signature"`
	Payload   ReleasePayload `json:"payload"`
}

type ReleasePolicy struct {
	Namespace        string
	Channel          string
	Keys             map[string]ed25519.PublicKey
	MaximumArtifacts int
	MaximumBytes     int64
	Now              func() time.Time
}

type ReleaseCatalog struct {
	KeyID    string
	Revision string
	Payload  ReleasePayload
}

type ReleaseVerification struct {
	SHA256     string
	Bytes      int64
	Inspection ExtractResult
}

// SignReleaseCatalog 供离线发布流水线生成规范 JSON 签名信封。
func SignReleaseCatalog(payload ReleasePayload, keyID string, privateKey ed25519.PrivateKey) ([]byte, error) {
	if !releaseNamePattern.MatchString(keyID) || len(privateKey) != ed25519.PrivateKeySize {
		return nil, ErrReleaseManifest
	}
	if err := validateReleasePayload(payload, 5000, defaultMaximumReleaseBytes); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrReleaseManifest
	}
	envelope := ReleaseEnvelope{KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical)), Payload: payload}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, ErrReleaseManifest
	}
	return data, nil
}

// ParseAndVerifyReleaseCatalog 严格解析并验证发布目录，未知字段和尾随内容都会被拒绝。
func ParseAndVerifyReleaseCatalog(data []byte, policy ReleasePolicy) (ReleaseCatalog, error) {
	if len(data) < 1 || len(data) > 2<<20 || !releaseNamePattern.MatchString(policy.Namespace) || !releaseNamePattern.MatchString(policy.Channel) {
		return ReleaseCatalog{}, ErrReleaseManifest
	}
	var envelope ReleaseEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return ReleaseCatalog{}, fmt.Errorf("%w: %v", ErrReleaseManifest, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ReleaseCatalog{}, ErrReleaseManifest
	}
	if !releaseNamePattern.MatchString(envelope.KeyID) {
		return ReleaseCatalog{}, ErrReleaseManifest
	}
	publicKey, trusted := policy.Keys[envelope.KeyID]
	if !trusted || len(publicKey) != ed25519.PublicKeySize {
		return ReleaseCatalog{}, ErrReleaseUntrusted
	}
	signature, decodeErr := base64.StdEncoding.Strict().DecodeString(envelope.Signature)
	canonical, marshalErr := json.Marshal(envelope.Payload)
	if decodeErr != nil || marshalErr != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, canonical, signature) {
		return ReleaseCatalog{}, ErrReleaseSignature
	}
	maximumArtifacts := policy.MaximumArtifacts
	if maximumArtifacts == 0 {
		maximumArtifacts = 5000
	}
	maximumBytes := policy.MaximumBytes
	if maximumBytes == 0 {
		maximumBytes = defaultMaximumReleaseBytes
	}
	if maximumArtifacts < 1 || maximumArtifacts > 10000 || maximumBytes < 1 || maximumBytes > defaultMaximumReleaseBytes {
		return ReleaseCatalog{}, ErrReleaseManifest
	}
	if err := validateReleasePayload(envelope.Payload, maximumArtifacts, maximumBytes); err != nil {
		return ReleaseCatalog{}, err
	}
	if envelope.Payload.Namespace != policy.Namespace || envelope.Payload.Channel != policy.Channel {
		return ReleaseCatalog{}, ErrReleasePolicy
	}
	now := time.Now
	if policy.Now != nil {
		now = policy.Now
	}
	generatedAt, _ := time.Parse(time.RFC3339, envelope.Payload.GeneratedAt)
	expiresAt, _ := time.Parse(time.RFC3339, envelope.Payload.ExpiresAt)
	current := now().UTC()
	if generatedAt.After(current.Add(5 * time.Minute)) {
		return ReleaseCatalog{}, ErrReleaseManifest
	}
	if !current.Before(expiresAt) {
		return ReleaseCatalog{}, ErrReleaseExpired
	}
	digest := sha256.Sum256(canonical)
	return ReleaseCatalog{KeyID: envelope.KeyID, Revision: "sha256:" + hex.EncodeToString(digest[:]), Payload: envelope.Payload}, nil
}

func (catalog ReleaseCatalog) Resolve(kind, id, version, goos, goarch string) (ReleaseArtifact, error) {
	if !releaseNamePattern.MatchString(kind) || !releaseNamePattern.MatchString(id) || !releaseVersionPattern.MatchString(version) ||
		!validReleaseOS(goos) || !validReleaseArch(goarch) || goos == "any" || goarch == "any" || goarch == "universal" {
		return ReleaseArtifact{}, ErrReleaseRequest
	}
	for _, release := range catalog.Payload.Artifacts {
		if release.Kind == kind && release.ID == id && release.Version == version && releasePlatformMatches(release, goos, goarch) {
			return release, nil
		}
	}
	return ReleaseArtifact{}, ErrReleaseNotFound
}

// VerifyReleaseArtifact 在使用前重新读取普通文件，并校验大小、摘要、格式与完整归档内容。
func VerifyReleaseArtifact(path string, release ReleaseArtifact, policy Policy) (ReleaseVerification, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ReleaseVerification{}, ErrReleaseArtifactMismatch
	}
	digest, bytesRead, err := fileSHA256(path)
	if err != nil || bytesRead != release.Bytes || subtle.ConstantTimeCompare([]byte(digest), []byte(release.SHA256)) != 1 {
		return ReleaseVerification{}, ErrReleaseArtifactMismatch
	}
	format := "." + release.Format
	inspection, err := InspectArchive(path, format, policy)
	if err != nil {
		return ReleaseVerification{}, errors.Join(ErrReleaseArtifactMismatch, err)
	}
	return ReleaseVerification{SHA256: digest, Bytes: bytesRead, Inspection: inspection}, nil
}

func validateReleasePayload(payload ReleasePayload, maximumArtifacts int, maximumBytes int64) error {
	if payload.SchemaVersion != 1 || !releaseNamePattern.MatchString(payload.Namespace) || !releaseNamePattern.MatchString(payload.Channel) ||
		len(payload.Artifacts) < 1 || len(payload.Artifacts) > maximumArtifacts {
		return ErrReleaseManifest
	}
	generatedAt, generatedErr := time.Parse(time.RFC3339, payload.GeneratedAt)
	expiresAt, expiresErr := time.Parse(time.RFC3339, payload.ExpiresAt)
	if generatedErr != nil || expiresErr != nil || !expiresAt.After(generatedAt) || expiresAt.Sub(generatedAt) > 7*24*time.Hour {
		return ErrReleaseManifest
	}
	seen := make(map[string]struct{}, len(payload.Artifacts))
	for _, release := range payload.Artifacts {
		if !validReleaseArtifact(release, maximumBytes) {
			return ErrReleaseManifest
		}
		key := strings.Join([]string{release.Kind, release.ID, release.Version, release.OS, release.Arch}, "\x00")
		if _, duplicate := seen[key]; duplicate {
			return ErrReleaseManifest
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validReleaseArtifact(release ReleaseArtifact, maximumBytes int64) bool {
	return releaseNamePattern.MatchString(release.ID) && releaseNamePattern.MatchString(release.Kind) && releaseVersionPattern.MatchString(release.Version) &&
		releaseDigestPattern.MatchString(release.SHA256) && release.Bytes > 0 && release.Bytes <= maximumBytes &&
		(release.Format == "zip" || release.Format == "tar.gz") && validReleaseOS(release.OS) && validReleaseArch(release.Arch) &&
		release.ContractVersion > 0 && release.ContractVersion <= 1024 && !(release.OS == "windows" && release.Arch == "universal")
}

func validReleaseOS(value string) bool {
	return value == "any" || value == "darwin" || value == "windows" || value == "linux"
}

func validReleaseArch(value string) bool {
	return value == "any" || value == "amd64" || value == "arm64" || value == "universal"
}

func releasePlatformMatches(release ReleaseArtifact, goos, goarch string) bool {
	osMatches := release.OS == "any" || release.OS == goos
	archMatches := release.Arch == "any" || release.Arch == goarch || release.OS == "darwin" && release.Arch == "universal" && goos == "darwin"
	return osMatches && archMatches
}

func fileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hasher := sha256.New()
	read, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), read, nil
}
