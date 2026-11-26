// Package media 提供受控本地文件的 MediaProvider 适配器。
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/contract"
	"github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

var (
	ErrDenied         = errors.New("媒体路径未被授权")
	ErrClosed         = errors.New("MediaProvider 已关闭")
	ErrUnknownSession = errors.New("播放会话不存在")
	ErrUnsupported    = errors.New("媒体类型或操作不受支持")
)

const defaultMaxBytes = 8 << 20

type Options struct {
	Root     string
	MaxBytes int64
	Info     contract.AdapterInfo
}

type playback struct {
	id       contract.PlaybackSessionID
	state    string
	position int64
}

type Provider struct {
	root     string
	maxBytes int64
	info     contract.AdapterInfo

	mu       sync.Mutex
	closed   bool
	nextID   uint64
	sessions map[contract.PlaybackSessionID]playback
}

var _ contract.MediaProvider = (*Provider)(nil)

func New(options Options) (*Provider, error) {
	root := filepath.Clean(strings.TrimSpace(options.Root))
	if root == "" || !filepath.IsAbs(root) {
		return nil, contract.ErrInvalidContract
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, contract.ErrInvalidContract
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, contract.ErrInvalidContract
	}
	if options.MaxBytes < 0 {
		return nil, contract.ErrInvalidContract
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = defaultMaxBytes
	}
	adapterInfo := options.Info
	if adapterInfo.ID == "" {
		version, err := model.ParseVersion("1.0.0")
		if err != nil {
			return nil, err
		}
		adapterInfo = contract.AdapterInfo{
			ID: "adapter.media.localfile", DisplayName: "Local File Media",
			BuildID: "localfile-1", Protocol: version,
			Capabilities: []contract.Capability{{ID: "adapter.contract", Stability: contract.StabilityExperimental}},
			Limitations:  []Limitation{{Code: "adapter.media.no-decode", MessageKey: "adapter.media.identity_thumbnail"}},
		}
	}
	if err := adapterInfo.Validate(); err != nil {
		return nil, err
	}
	return &Provider{
		root: resolved, maxBytes: options.MaxBytes, info: adapterInfo,
		sessions: make(map[contract.PlaybackSessionID]playback),
	}, nil
}

type Limitation = contract.Limitation

func (provider *Provider) Info(context.Context) (contract.AdapterInfo, error) {
	if provider == nil {
		return contract.AdapterInfo{}, contract.ErrInvalidContract
	}
	return provider.info, nil
}

func (provider *Provider) Health(context.Context) (health.Report, error) {
	if provider == nil {
		return health.Report{}, contract.ErrInvalidContract
	}
	provider.mu.Lock()
	closed := provider.closed
	provider.mu.Unlock()
	observed, err := model.NewTimestamp(time.Now().UTC())
	if err != nil {
		return health.Report{}, err
	}
	status := health.StatusHealthy
	if closed {
		status = health.StatusUnhealthy
	}
	return health.Report{Status: status, ObservedAt: observed, Build: provider.info.Protocol}, nil
}

func (provider *Provider) Close(context.Context) error {
	if provider == nil {
		return contract.ErrInvalidContract
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.closed = true
	provider.sessions = map[contract.PlaybackSessionID]playback{}
	return nil
}

func (provider *Provider) Inspect(ctx context.Context, source contract.MediaSource) (contract.MediaInfo, error) {
	path, err := provider.resolve(ctx, source)
	if err != nil {
		return contract.MediaInfo{}, err
	}
	stat, err := os.Lstat(path)
	if err != nil {
		return contract.MediaInfo{}, ErrDenied
	}
	return contract.MediaInfo{MIMEType: mimeFromExt(path), Bytes: stat.Size()}, nil
}

func (provider *Provider) Thumbnail(ctx context.Context, request contract.ThumbnailRequest) (contract.ArtifactRef, error) {
	if request.Width < 1 || request.Width > 4096 || request.Height < 1 || request.Height > 4096 {
		return contract.ArtifactRef{}, contract.ErrInvalidContract
	}
	path, err := provider.resolve(ctx, request.Source)
	if err != nil {
		return contract.ArtifactRef{}, err
	}
	if !strings.HasPrefix(mimeFromExt(path), "image/") {
		return contract.ArtifactRef{}, ErrUnsupported
	}
	digest, size, err := hashFile(path, provider.maxBytes)
	if err != nil {
		return contract.ArtifactRef{}, err
	}
	return contract.ArtifactRef{ID: model.ID("thumb-" + digest[:12]), SHA256: digest, Bytes: size}, nil
}

func (provider *Provider) OpenPlayback(ctx context.Context, request contract.PlaybackRequest) (contract.PlaybackSession, error) {
	if _, err := provider.resolve(ctx, request.Source); err != nil {
		return contract.PlaybackSession{}, err
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.closed {
		return contract.PlaybackSession{}, ErrClosed
	}
	provider.nextID++
	id := contract.PlaybackSessionID(fmt.Sprintf("play-%d", provider.nextID))
	provider.sessions[id] = playback{id: id, state: "playing"}
	return contract.PlaybackSession{ID: id}, nil
}

func (provider *Provider) Control(_ context.Context, command contract.PlaybackCommand) (contract.PlaybackState, error) {
	if provider == nil {
		return contract.PlaybackState{}, contract.ErrInvalidContract
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.closed {
		return contract.PlaybackState{}, ErrClosed
	}
	session, exists := provider.sessions[command.SessionID]
	if !exists {
		return contract.PlaybackState{}, ErrUnknownSession
	}
	switch command.Kind {
	case "play":
		session.state = "playing"
	case "pause":
		session.state = "paused"
	case "stop":
		session.state = "stopped"
		session.position = 0
	default:
		return contract.PlaybackState{}, ErrUnsupported
	}
	provider.sessions[command.SessionID] = session
	return contract.PlaybackState{SessionID: session.id, State: session.state, Position: session.position}, nil
}

func (provider *Provider) ClosePlayback(_ context.Context, sessionID contract.PlaybackSessionID) error {
	if provider == nil || sessionID == "" {
		return contract.ErrInvalidContract
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.closed {
		return ErrClosed
	}
	if _, exists := provider.sessions[sessionID]; !exists {
		return ErrUnknownSession
	}
	delete(provider.sessions, sessionID)
	return nil
}

func (provider *Provider) resolve(ctx context.Context, source contract.MediaSource) (string, error) {
	if provider == nil || ctx == nil {
		return "", contract.ErrInvalidContract
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := source.Meta.Validate(time.Now().UTC(), false); err != nil {
		return "", contract.ErrInvalidContract
	}
	provider.mu.Lock()
	closed := provider.closed
	provider.mu.Unlock()
	if closed {
		return "", ErrClosed
	}
	path, err := parseMediaURI(source.URI)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > provider.maxBytes {
		return "", ErrDenied
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", ErrDenied
	}
	rel, err := filepath.Rel(provider.root, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrDenied
	}
	return resolved, nil
}

func parseMediaURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrDenied
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "file" || (parsed.Host != "" && parsed.Host != "localhost") {
			return "", ErrDenied
		}
		raw = parsed.Path
		if runtime.GOOS == "windows" && len(raw) >= 3 && raw[0] == '/' && raw[2] == ':' {
			raw = raw[1:]
		}
		if decoded, err := url.PathUnescape(raw); err == nil {
			raw = decoded
		}
	}
	if !filepath.IsAbs(raw) {
		return "", ErrDenied
	}
	return filepath.Clean(raw), nil
}

func mimeFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".wav":
		return "audio/wav"
	case ".mp4":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

func hashFile(path string, maxBytes int64) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, ErrDenied
	}
	defer file.Close()
	sum := sha256.New()
	written, err := io.Copy(sum, io.LimitReader(file, maxBytes+1))
	if err != nil || written > maxBytes {
		return "", 0, ErrDenied
	}
	return hex.EncodeToString(sum.Sum(nil)), written, nil
}
