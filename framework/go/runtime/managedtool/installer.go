// Package managedtool 提供产品无关的可信工具包私有安装与原子切换能力。
package managedtool

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/artifact"
	"github.com/heiyeluren/mz-gui/framework/go/storage/directorytxn"
)

const FormatZIP = "zip"

const (
	defaultMaxPackageBytes   int64 = 1 << 30
	defaultMaxExtractedBytes int64 = 2 << 30
	defaultMaxEntries              = 20_000
	defaultMaxPathBytes            = 512
)

var (
	ErrInvalidOptions = errors.New("受管工具安装器配置无效")
	ErrInvalidPackage = errors.New("受管工具包无效")
	ErrNotInstalled   = errors.New("受管工具尚未安装")
	componentPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	versionPattern    = regexp.MustCompile(`^[0-9][0-9A-Za-z.+_-]{0,63}$`)
	sha256Pattern     = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type Package struct {
	ID         string
	Version    string
	URL        string
	Format     string
	SHA256     string
	Size       int64
	Executable string
}

type Options struct {
	Root              string
	PackageRoot       string
	AllowedHosts      []string
	Downloader        artifact.Downloader
	MaxPackageBytes   int64
	MaxExtractedBytes int64
	MaxEntries        int
	MaxPathBytes      int
}

type Result struct {
	ID             string
	Version        string
	ExecutablePath string
	Bytes          int64
	SHA256         string
	Reused         bool
}

type Installer struct {
	options Options
	txn     *directorytxn.Manager
	gate    chan struct{}
}

func New(options Options) (*Installer, error) {
	options.Root = filepath.Clean(strings.TrimSpace(options.Root))
	options.PackageRoot = filepath.Clean(strings.TrimSpace(options.PackageRoot))
	if !filepath.IsAbs(options.Root) || !filepath.IsAbs(options.PackageRoot) || options.Root == options.PackageRoot || len(options.AllowedHosts) == 0 {
		return nil, ErrInvalidOptions
	}
	if options.MaxPackageBytes <= 0 {
		options.MaxPackageBytes = defaultMaxPackageBytes
	}
	if options.MaxExtractedBytes <= 0 {
		options.MaxExtractedBytes = defaultMaxExtractedBytes
	}
	if options.MaxEntries <= 0 {
		options.MaxEntries = defaultMaxEntries
	}
	if options.MaxPathBytes <= 0 {
		options.MaxPathBytes = defaultMaxPathBytes
	}
	if options.MaxPackageBytes > 4<<30 || options.MaxExtractedBytes > 8<<30 || options.MaxEntries > 100_000 || options.MaxPathBytes > 4096 {
		return nil, ErrInvalidOptions
	}
	for _, host := range options.AllowedHosts {
		if strings.TrimSpace(host) == "" || strings.ContainsAny(host, "/\\@") {
			return nil, ErrInvalidOptions
		}
	}
	if err := ensurePrivateDirectory(options.PackageRoot); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOptions, err)
	}
	txn, err := directorytxn.New(directorytxn.Options{Root: options.Root})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOptions, err)
	}
	return &Installer{options: options, txn: txn, gate: make(chan struct{}, 1)}, nil
}

func (installer *Installer) Install(ctx context.Context, request Package) (Result, error) {
	if ctx == nil || installer == nil || installer.txn == nil || validatePackage(request, installer.options.MaxPackageBytes) != nil {
		return Result{}, ErrInvalidPackage
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	select {
	case installer.gate <- struct{}{}:
		defer func() { <-installer.gate }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	if executable, err := installer.Resolve(request.ID, request.Executable); err == nil {
		metadata, metadataErr := readMetadata(installer.txn.TargetPath(request.ID))
		if metadataErr == nil && metadata.Version == request.Version && metadata.SHA256 == request.SHA256 && metadata.Executable == request.Executable {
			return Result{ID: request.ID, Version: request.Version, ExecutablePath: executable, Bytes: request.Size, SHA256: request.SHA256, Reused: true}, nil
		}
	}
	packagePath := filepath.Join(installer.options.PackageRoot, request.ID+"-"+request.Version+"-"+request.SHA256[:16]+".zip")
	if reused, err := verifyFile(packagePath, request.Size, request.SHA256); err != nil {
		return Result{}, fmt.Errorf("验证 Runtime 缓存包: %w", err)
	} else if !reused {
		if _, err := installer.options.Downloader.Download(ctx, request.URL, packagePath, request.SHA256, artifact.Policy{
			AllowedHosts: installer.options.AllowedHosts, MaxBytes: installer.options.MaxPackageBytes, ExpectedBytes: request.Size,
		}); err != nil {
			return Result{}, fmt.Errorf("下载 Runtime 包: %w", err)
		}
	}
	stagingID, backupID, err := transactionIDs(request.ID)
	if err != nil {
		return Result{}, err
	}
	stagingPath := installer.txn.StagingPath(stagingID)
	if _, err := artifact.ExtractZIP(packagePath, stagingPath, artifact.Policy{
		MaxEntries: installer.options.MaxEntries, MaxExtractedBytes: installer.options.MaxExtractedBytes, MaxPathBytes: installer.options.MaxPathBytes,
	}); err != nil {
		return Result{}, fmt.Errorf("解压 Runtime 包: %w", err)
	}
	executablePath := filepath.Join(stagingPath, filepath.FromSlash(request.Executable))
	if err := validateExecutable(executablePath); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(executablePath, 0o700); err != nil {
		return Result{}, fmt.Errorf("设置 Runtime 可执行权限: %w", err)
	}
	if err := writeMetadata(stagingPath, metadata{Version: request.Version, SHA256: request.SHA256, Executable: request.Executable}); err != nil {
		return Result{}, err
	}
	committed, err := installer.txn.Replace(ctx, directorytxn.ReplaceRequest{Key: request.ID, StagingID: stagingID, BackupID: backupID})
	if err != nil {
		return Result{}, fmt.Errorf("发布 Runtime: %w", err)
	}
	return Result{
		ID: request.ID, Version: request.Version,
		ExecutablePath: filepath.Join(committed.TargetPath, filepath.FromSlash(request.Executable)),
		Bytes:          request.Size, SHA256: request.SHA256,
	}, nil
}

func (installer *Installer) Resolve(id, executable string) (string, error) {
	if installer == nil || installer.txn == nil || !componentPattern.MatchString(id) || !safeRelativePath(executable) {
		return "", ErrInvalidPackage
	}
	target := filepath.Join(installer.txn.TargetPath(id), filepath.FromSlash(executable))
	if err := validateExecutable(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotInstalled
		}
		return "", err
	}
	return target, nil
}

// Inspect 返回已经原子发布的受管工具元数据，不执行目标二进制。
func (installer *Installer) Inspect(id, executable string) (Result, error) {
	target, err := installer.Resolve(id, executable)
	if err != nil {
		return Result{}, err
	}
	metadata, err := readMetadata(installer.txn.TargetPath(id))
	if err != nil || metadata.Executable != executable {
		return Result{}, ErrInvalidPackage
	}
	return Result{ID: id, Version: metadata.Version, ExecutablePath: target, SHA256: metadata.SHA256, Reused: true}, nil
}

func validatePackage(request Package, maxBytes int64) error {
	parsed, err := url.Parse(request.URL)
	if !componentPattern.MatchString(request.ID) || !versionPattern.MatchString(request.Version) || request.Format != FormatZIP ||
		!sha256Pattern.MatchString(request.SHA256) || request.Size < 1 || request.Size > maxBytes || !safeRelativePath(request.Executable) ||
		err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || !strings.HasSuffix(strings.ToLower(parsed.Path), ".zip") {
		return ErrInvalidPackage
	}
	return nil
}

func safeRelativePath(value string) bool {
	if value == "" || len(value) > defaultMaxPathBytes || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func validateExecutable(target string) error {
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 {
		return errors.New("Runtime 可执行文件类型无效")
	}
	return nil
}

func ensurePrivateDirectory(target string) error {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(target, 0o700); err != nil {
			return err
		}
		return os.Chmod(target, 0o700)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Runtime 包目录类型不安全")
	}
	return os.Chmod(target, 0o700)
}

func verifyFile(target string, size int64, expected string) (bool, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != size {
		return false, errors.New("缓存包与签名清单不一致")
	}
	file, err := os.Open(target)
	if err != nil {
		return false, err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return false, err
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return false, errors.New("缓存包摘要不匹配")
	}
	return true, nil
}

type metadata struct {
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	Executable string `json:"executable"`
}

func writeMetadata(root string, value metadata) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("编码 Runtime 安装元数据: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mz-managed-tool"), data, 0o600); err != nil {
		return fmt.Errorf("写入 Runtime 安装元数据: %w", err)
	}
	return nil
}

func readMetadata(root string) (metadata, error) {
	data, err := os.ReadFile(filepath.Join(root, ".mz-managed-tool"))
	if err != nil {
		return metadata{}, err
	}
	value := metadata{}
	if len(data) > 4096 || json.Unmarshal(data, &value) != nil || !versionPattern.MatchString(value.Version) ||
		!sha256Pattern.MatchString(value.SHA256) || !safeRelativePath(value.Executable) {
		return metadata{}, ErrInvalidPackage
	}
	return value, nil
}

func transactionIDs(id string) (string, string, error) {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", fmt.Errorf("生成 Runtime 事务标识: %w", err)
	}
	suffix := hex.EncodeToString(buffer)
	return id + "-stage-" + suffix, id + "-backup-" + suffix, nil
}
