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
	"sort"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/platform/toolruntime"
)

const (
	runtimeID       = "git-bash"
	maxRuntimeFiles = 20_000
)

type releaseManifest struct {
	SchemaVersion int               `json:"schema_version"`
	RuntimeID     string            `json:"runtime_id"`
	Version       string            `json:"version"`
	Artifacts     []releaseArtifact `json:"artifacts"`
}

type releaseArtifact struct {
	Arch       string `json:"arch"`
	VendorArch string `json:"vendor_arch"`
	URL        string `json:"url"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
}

type prepareOptions struct {
	ManifestPath string
	Arch         string
	CacheRoot    string
	HTTPClient   *http.Client
	Extract      func(context.Context, string, string) error
}

func main() {
	manifestPath := flag.String("manifest", "docs/release/git-bash-runtime-v1.json", "Git Bash 发布清单")
	arch := flag.String("arch", runtime.GOARCH, "目标架构：amd64 或 arm64")
	cacheRoot := flag.String("cache", defaultCacheRoot(), "下载与解包缓存目录")
	output := flag.String("output", "all", "输出格式：all、path 或 digest")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	path, digest, err := prepare(ctx, prepareOptions{ManifestPath: *manifestPath, Arch: *arch, CacheRoot: *cacheRoot})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "准备 Git Bash Runtime 失败: %v\n", err)
		os.Exit(1)
	}
	switch *output {
	case "all":
		fmt.Printf("%s\n%s\n", path, digest)
	case "path":
		fmt.Println(path)
	case "digest":
		fmt.Println(digest)
	default:
		_, _ = fmt.Fprintln(os.Stderr, "Git Bash Runtime 输出格式无效")
		os.Exit(2)
	}
}

func prepare(ctx context.Context, options prepareOptions) (string, string, error) {
	manifest, artifact, err := loadRelease(options.ManifestPath, options.Arch)
	if err != nil {
		return "", "", err
	}
	if !filepath.IsAbs(options.CacheRoot) {
		return "", "", errors.New("Git Bash 缓存目录必须是绝对路径")
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 30 * time.Minute}
	}
	if options.Extract == nil {
		options.Extract = extract7Zip
	}
	versionRoot := filepath.Join(options.CacheRoot, manifest.Version)
	archiveDirectory := filepath.Join(versionRoot, "archive")
	archiveName := filepath.Base(mustURLPath(artifact.URL))
	archivePath := filepath.Join(archiveDirectory, archiveName)
	if err := os.MkdirAll(archiveDirectory, 0o700); err != nil {
		return "", "", err
	}
	if err := verifyFile(archivePath, artifact.Size, artifact.SHA256); err != nil {
		if _, statErr := os.Lstat(archivePath); statErr == nil {
			if err := quarantine(archivePath); err != nil {
				return "", "", err
			}
		} else if !os.IsNotExist(statErr) {
			return "", "", statErr
		}
		if err := download(ctx, options.HTTPClient, artifact, archivePath); err != nil {
			return "", "", err
		}
	}
	temporaryRoot := filepath.Join(options.CacheRoot, "tmp")
	if err := os.MkdirAll(temporaryRoot, 0o700); err != nil {
		return "", "", err
	}
	staging, err := os.MkdirTemp(temporaryRoot, "git-bash-prepare-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(staging)
	if err := options.Extract(ctx, archivePath, staging); err != nil {
		return "", "", fmt.Errorf("解包 Git Bash Runtime: %w", err)
	}
	if err := validateExtractedTree(staging); err != nil {
		return "", "", err
	}
	vendorRoot := map[string]string{"amd64": "mingw64", "arm64": "clangarm64"}[artifact.Arch]
	if err := prepareGitDLLs(staging, vendorRoot); err != nil {
		return "", "", err
	}
	protectedRoots := []string{"cmd", vendorRoot + "/bin", vendorRoot + "/libexec/git-core", "usr/bin"}
	entrypointArch := "amd64"
	descriptor, digest, err := buildRuntimeManifest(staging, manifest.Version, artifact.Arch, entrypointArch, protectedRoots)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(staging, toolruntime.ManifestName), descriptor, 0o600); err != nil {
		return "", "", err
	}
	if _, err := toolruntime.ValidateDirectory(staging, runtimeID, manifest.Version, artifact.Arch, digest); err != nil {
		return "", "", fmt.Errorf("校验 Git Bash Runtime: %w", err)
	}
	runtimeArchRoot := filepath.Join(versionRoot, "runtime", artifact.Arch)
	runtimeDirectory := filepath.Join(runtimeArchRoot, runtimeID)
	if err := os.MkdirAll(runtimeArchRoot, 0o700); err != nil {
		return "", "", err
	}
	if err := os.RemoveAll(runtimeDirectory); err != nil {
		return "", "", fmt.Errorf("清理旧 Git Bash Runtime 缓存: %w", err)
	}
	if err := os.Rename(staging, runtimeDirectory); err != nil {
		return "", "", fmt.Errorf("发布 Git Bash Runtime 缓存: %w", err)
	}
	return runtimeDirectory, digest, nil
}

func loadRelease(path, arch string) (releaseManifest, releaseArtifact, error) {
	file, err := os.Open(path)
	if err != nil {
		return releaseManifest{}, releaseArtifact{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest releaseManifest
	if err := decoder.Decode(&manifest); err != nil {
		return releaseManifest{}, releaseArtifact{}, fmt.Errorf("解析 Git Bash 发布清单: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return releaseManifest{}, releaseArtifact{}, errors.New("Git Bash 发布清单包含多余 JSON 值")
	}
	if manifest.SchemaVersion != 1 || manifest.RuntimeID != runtimeID || !validComponent(manifest.Version) {
		return releaseManifest{}, releaseArtifact{}, errors.New("Git Bash 发布清单头无效")
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	var selected releaseArtifact
	for _, artifact := range manifest.Artifacts {
		if _, exists := seen[artifact.Arch]; exists {
			return releaseManifest{}, releaseArtifact{}, errors.New("Git Bash 发布清单架构重复")
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
		return releaseManifest{}, releaseArtifact{}, errors.New("Git Bash 发布清单必须包含 amd64 和 arm64")
	}
	if selected.Arch == "" {
		return releaseManifest{}, releaseArtifact{}, fmt.Errorf("Git Bash 发布清单不包含架构 %q", arch)
	}
	return manifest, selected, nil
}

func validateArtifact(version string, artifact releaseArtifact) error {
	wantVendor := map[string]string{"amd64": "64-bit", "arm64": "arm64"}[artifact.Arch]
	if wantVendor == "" || artifact.VendorArch != wantVendor {
		return errors.New("Git Bash 制品架构映射无效")
	}
	parsed, err := url.Parse(artifact.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" {
		return errors.New("Git Bash 制品必须来自 GitHub HTTPS")
	}
	wantName := fmt.Sprintf("PortableGit-%s-%s.7z.exe", version, artifact.VendorArch)
	if filepath.Base(parsed.Path) != wantName || artifact.Size <= 0 || !validDigest(artifact.SHA256) {
		return errors.New("Git Bash 制品元数据无效")
	}
	return nil
}

func buildRuntimeManifest(root, version, arch, entrypointArch string, protectedRoots []string) ([]byte, string, error) {
	manifest := toolruntime.Manifest{SchemaVersion: 1, RuntimeID: runtimeID, Version: version, Arch: arch,
		Entrypoint: "usr/bin/bash.exe", EntrypointArch: entrypointArch, ProtectedRoots: append([]string(nil), protectedRoots...)}
	sort.Strings(manifest.ProtectedRoots)
	for _, protectedRoot := range manifest.ProtectedRoots {
		directory := filepath.Join(root, filepath.FromSlash(protectedRoot))
		if info, err := os.Lstat(directory); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, "", fmt.Errorf("Git Bash 保护目录无效: %s", protectedRoot)
		}
		err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 {
				return errors.New("Git Bash 保护目录包含无效文件")
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			digest, err := hashFile(path)
			if err != nil {
				return err
			}
			manifest.Files = append(manifest.Files, toolruntime.FileRecord{Path: filepath.ToSlash(relative), Size: info.Size(), SHA256: digest})
			return nil
		})
		if err != nil {
			return nil, "", err
		}
	}
	toolruntime.SortFileRecords(manifest.Files)
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(data)
	return data, hex.EncodeToString(digest[:]), nil
}

func prepareGitDLLs(root, vendorRoot string) error {
	sourceDirectory := filepath.Join(root, vendorRoot, "bin")
	targetDirectory := filepath.Join(root, vendorRoot, "libexec", "git-core")
	gitPath := filepath.Join(sourceDirectory, "git.exe")
	if err := copyFile(gitPath, filepath.Join(targetDirectory, "dlls-copied")); err != nil {
		return fmt.Errorf("准备 Git DLL 标记: %w", err)
	}
	entries, err := os.ReadDir(sourceDirectory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".dll") {
			continue
		}
		if err := copyFile(filepath.Join(sourceDirectory, entry.Name()), filepath.Join(targetDirectory, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func validateExtractedTree(root string) error {
	count := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		count++
		if count > maxRuntimeFiles {
			return errors.New("Git Bash 解包文件数超过限制")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("Git Bash 解包结果包含符号链接或特殊文件")
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, required := range []string{"post-install.bat", "usr/bin/bash.exe", "usr/bin/msys-2.0.dll"} {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(required))); err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			return fmt.Errorf("Git Bash 缺少关键文件 %s", required)
		}
	}
	return nil
}

func extract7Zip(ctx context.Context, archive, destination string) error {
	program, err := exec.LookPath("7zz")
	if err != nil {
		program, err = exec.LookPath("7z")
	}
	if err != nil {
		return errors.New("缺少 7-Zip CLI（7zz 或 7z）")
	}
	command := exec.CommandContext(ctx, program, "x", "-y", "-o"+destination, archive)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
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
		return err
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL == nil || response.Request.URL.Scheme != "https" || response.StatusCode != http.StatusOK {
		return errors.New("Git Bash 下载响应不可信")
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".git-bash-download-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(response.Body, artifact.Size+1))
	if copyErr != nil || written != artifact.Size || hex.EncodeToString(hasher.Sum(nil)) != artifact.SHA256 {
		temporary.Close()
		return errors.New("Git Bash 制品大小或 SHA-256 不匹配")
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, destination)
}

func verifyFile(path string, size int64, expectedHash string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("Git Bash 缓存制品无效")
	}
	digest, err := hashFile(path)
	if err != nil || digest != expectedHash {
		return errors.New("Git Bash 缓存制品摘要无效")
	}
	return nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func quarantine(path string) error {
	return os.Rename(path, fmt.Sprintf("%s.invalid-%d", path, time.Now().UTC().UnixNano()))
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func validComponent(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return value[0] != '.' && value[0] != '-'
}

func mustURLPath(raw string) string {
	parsed, _ := url.Parse(raw)
	return parsed.Path
}

func defaultCacheRoot() string {
	if value := strings.TrimSpace(os.Getenv("MZ_GUI_GIT_BASH_CACHE")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".cache", "mz-gui", "git-bash")
	}
	return filepath.Join(home, ".cache", "mz-gui", "git-bash")
}
