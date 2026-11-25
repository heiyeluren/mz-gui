package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/platform/webviewruntime"
)

type releaseManifest struct {
	Schema        string            `json:"$schema,omitempty"`
	SchemaVersion int               `json:"schema_version"`
	Version       string            `json:"version"`
	Artifacts     []releaseArtifact `json:"artifacts"`
}

type releaseArtifact struct {
	Arch        string `json:"arch"`
	VendorArch  string `json:"vendor_arch"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	ArchiveRoot string `json:"archive_root"`
}

type prepareOptions struct {
	ManifestPath string
	Arch         string
	CacheRoot    string
	HTTPClient   *http.Client
	Extract      func(context.Context, string, string) error
}

func main() {
	manifestPath := flag.String("manifest", "docs/release/webview2-fixed-runtime-v1.json", "固定版 WebView2 发布清单")
	arch := flag.String("arch", runtime.GOARCH, "目标架构：amd64 或 arm64")
	cacheRoot := flag.String("cache", defaultCacheRoot(), "下载与解包缓存目录")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	path, err := prepare(ctx, prepareOptions{ManifestPath: *manifestPath, Arch: *arch, CacheRoot: *cacheRoot})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "准备固定版 WebView2 Runtime 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(path)
}

func prepare(ctx context.Context, options prepareOptions) (string, error) {
	manifest, artifact, err := loadRelease(options.ManifestPath, options.Arch)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(options.CacheRoot) {
		return "", errors.New("WebView2 缓存目录必须是绝对路径")
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 30 * time.Minute}
	}
	if options.Extract == nil {
		options.Extract = extractCAB
	}

	versionRoot := filepath.Join(options.CacheRoot, manifest.Version)
	archiveDirectory := filepath.Join(versionRoot, "cab")
	runtimeArchDirectory := filepath.Join(versionRoot, "runtime", artifact.Arch)
	runtimeDirectory := filepath.Join(runtimeArchDirectory, artifact.ArchiveRoot)
	if err := webviewruntime.ValidateDirectory(runtimeDirectory, manifest.Version, artifact.Arch); err == nil {
		return runtimeDirectory, nil
	} else if _, statErr := os.Lstat(runtimeDirectory); statErr == nil {
		if err := quarantine(runtimeDirectory); err != nil {
			return "", fmt.Errorf("隔离损坏的 WebView2 目录: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}

	parsedURL, err := url.Parse(artifact.URL)
	if err != nil {
		return "", err
	}
	archiveName := filepath.Base(parsedURL.Path)
	if !strings.HasSuffix(strings.ToLower(archiveName), ".cab") || archiveName == "." {
		return "", errors.New("WebView2 制品 URL 缺少 CAB 文件名")
	}
	if err := os.MkdirAll(archiveDirectory, 0o700); err != nil {
		return "", fmt.Errorf("创建 WebView2 CAB 缓存目录: %w", err)
	}
	archivePath := filepath.Join(archiveDirectory, archiveName)
	if err := verifyFile(archivePath, artifact.Size, artifact.SHA256); err != nil {
		if _, statErr := os.Lstat(archivePath); statErr == nil {
			if err := quarantine(archivePath); err != nil {
				return "", fmt.Errorf("隔离损坏的 WebView2 CAB: %w", err)
			}
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
		if err := download(ctx, options.HTTPClient, artifact, archivePath); err != nil {
			return "", err
		}
	}

	temporaryRoot := filepath.Join(options.CacheRoot, "tmp")
	if err := os.MkdirAll(temporaryRoot, 0o700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(temporaryRoot, "webview2-prepare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if err := options.Extract(ctx, archivePath, staging); err != nil {
		return "", fmt.Errorf("解包 WebView2 CAB: %w", err)
	}
	extracted := filepath.Join(staging, artifact.ArchiveRoot)
	if err := webviewruntime.ValidateDirectory(extracted, manifest.Version, artifact.Arch); err != nil {
		return "", fmt.Errorf("校验解包后的 WebView2 Runtime: %w", err)
	}
	if err := os.MkdirAll(runtimeArchDirectory, 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(extracted, runtimeDirectory); err != nil {
		return "", fmt.Errorf("发布 WebView2 Runtime 缓存: %w", err)
	}
	return runtimeDirectory, nil
}

func loadRelease(path, arch string) (releaseManifest, releaseArtifact, error) {
	file, err := os.Open(path)
	if err != nil {
		return releaseManifest{}, releaseArtifact{}, fmt.Errorf("读取 WebView2 发布清单: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest releaseManifest
	if err := decoder.Decode(&manifest); err != nil {
		return releaseManifest{}, releaseArtifact{}, fmt.Errorf("解析 WebView2 发布清单: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return releaseManifest{}, releaseArtifact{}, err
	}
	if manifest.SchemaVersion != 1 {
		return releaseManifest{}, releaseArtifact{}, errors.New("WebView2 发布清单 schema_version 必须为 1")
	}
	if strings.TrimSpace(manifest.Version) != manifest.Version || !fourPartVersion(manifest.Version) {
		return releaseManifest{}, releaseArtifact{}, errors.New("WebView2 发布清单版本无效")
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	var selected releaseArtifact
	for _, artifact := range manifest.Artifacts {
		if _, exists := seen[artifact.Arch]; exists {
			return releaseManifest{}, releaseArtifact{}, fmt.Errorf("WebView2 发布清单架构重复: %s", artifact.Arch)
		}
		seen[artifact.Arch] = struct{}{}
		if err := validateArtifact(manifest.Version, artifact); err != nil {
			return releaseManifest{}, releaseArtifact{}, err
		}
		if artifact.Arch == arch {
			selected = artifact
		}
	}
	if len(seen) != 2 {
		return releaseManifest{}, releaseArtifact{}, errors.New("WebView2 发布清单必须同时包含 amd64 和 arm64")
	}
	for _, requiredArch := range []string{"amd64", "arm64"} {
		if _, exists := seen[requiredArch]; !exists {
			return releaseManifest{}, releaseArtifact{}, fmt.Errorf("WebView2 发布清单缺少架构 %s", requiredArch)
		}
	}
	if selected.Arch == "" {
		return releaseManifest{}, releaseArtifact{}, fmt.Errorf("WebView2 发布清单不包含架构 %q", arch)
	}
	return manifest, selected, nil
}

func validateArtifact(version string, artifact releaseArtifact) error {
	wantVendorArch := map[string]string{"amd64": "x64", "arm64": "arm64"}[artifact.Arch]
	if wantVendorArch == "" || artifact.VendorArch != wantVendorArch {
		return fmt.Errorf("WebView2 制品架构映射无效: %s/%s", artifact.Arch, artifact.VendorArch)
	}
	parsed, err := url.Parse(artifact.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("WebView2 制品 URL 必须是 HTTPS")
	}
	if artifact.Size <= 0 {
		return errors.New("WebView2 制品大小无效")
	}
	decodedHash, err := hex.DecodeString(artifact.SHA256)
	if err != nil || len(decodedHash) != sha256.Size || strings.ToLower(artifact.SHA256) != artifact.SHA256 {
		return errors.New("WebView2 制品 SHA-256 无效")
	}
	wantRoot := fmt.Sprintf("Microsoft.WebView2.FixedVersionRuntime.%s.%s", version, artifact.VendorArch)
	if artifact.ArchiveRoot != wantRoot || filepath.Base(artifact.ArchiveRoot) != artifact.ArchiveRoot {
		return errors.New("WebView2 CAB 顶层目录无效")
	}
	return nil
}

func download(ctx context.Context, client *http.Client, artifact releaseArtifact, destination string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("下载 WebView2 CAB: %w", err)
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL == nil || response.Request.URL.Scheme != "https" {
		return errors.New("WebView2 CAB 下载发生非 HTTPS 重定向")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 WebView2 CAB: HTTP %d", response.StatusCode)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".webview2-download-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(response.Body, artifact.Size+1))
	if copyErr != nil {
		temporary.Close()
		return fmt.Errorf("写入 WebView2 CAB: %w", copyErr)
	}
	if written != artifact.Size || hex.EncodeToString(hasher.Sum(nil)) != artifact.SHA256 {
		temporary.Close()
		return errors.New("WebView2 CAB 大小或 SHA-256 不匹配")
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("发布 WebView2 CAB 缓存: %w", err)
	}
	return nil
}

func verifyFile(path string, size int64, expectedHash string) error {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 || !linkInfo.Mode().IsRegular() {
		return errors.New("缓存文件必须是普通文件且不能是符号链接")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("缓存文件大小无效")
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	if hex.EncodeToString(hasher.Sum(nil)) != expectedHash {
		return errors.New("缓存文件 SHA-256 无效")
	}
	return nil
}

func extractCAB(ctx context.Context, archive, destination string) error {
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(ctx, "expand.exe", archive, "-F:*", destination)
	} else {
		command = exec.CommandContext(ctx, "bsdtar", "-xf", archive, "-C", destination)
	}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func quarantine(path string) error {
	return os.Rename(path, fmt.Sprintf("%s.invalid-%d", path, time.Now().UTC().UnixNano()))
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("WebView2 发布清单包含多个 JSON 值")
		}
		return fmt.Errorf("解析 WebView2 发布清单尾部: %w", err)
	}
	return nil
}

func fourPartVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func defaultCacheRoot() string {
	if value := strings.TrimSpace(os.Getenv("MZ_GUI_WEBVIEW2_CACHE")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".cache", "mz-gui", "webview2")
	}
	return filepath.Join(home, ".cache", "mz-gui", "webview2")
}
