package artifact

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Policy struct {
	AllowedHosts      []string
	PathPrefix        string
	PathPrefixes      []string
	MaxBytes          int64
	ExpectedBytes     int64
	MaxEntries        int
	MaxExtractedBytes int64
	MaxPathBytes      int
}

type DownloadResult struct {
	Bytes  int64
	SHA256 string
}

type ExtractResult struct {
	Entries int
	Bytes   int64
}

type HTTPError struct {
	StatusCode int
}

func (err *HTTPError) Error() string {
	return fmt.Sprintf("制品请求返回 HTTP %d", err.StatusCode)
}

func HTTPStatusCode(err error) (int, bool) {
	var statusError *HTTPError
	if !errors.As(err, &statusError) || statusError.StatusCode < 100 || statusError.StatusCode > 599 {
		return 0, false
	}
	return statusError.StatusCode, true
}

type Downloader struct {
	Client    *http.Client
	Authorize func(context.Context, *http.Request) error
}

func (downloader Downloader) Fetch(ctx context.Context, rawURL string, policy Policy) ([]byte, error) {
	if ctx == nil || policy.MaxBytes < 1 {
		return nil, errors.New("制品元数据请求参数无效")
	}
	source, err := validateURL(rawURL, policy)
	if err != nil {
		return nil, err
	}
	client := downloader.secureClient(policy)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("创建制品元数据请求: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if err := downloader.authorize(ctx, request); err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("读取制品元数据: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: response.StatusCode}
	}
	if _, err := validateURL(response.Request.URL.String(), policy); err != nil {
		return nil, err
	}
	if response.ContentLength > policy.MaxBytes {
		return nil, errors.New("制品元数据超过安全上限")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, policy.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取制品元数据正文: %w", err)
	}
	if int64(len(data)) > policy.MaxBytes {
		return nil, errors.New("制品元数据超过安全上限")
	}
	return data, nil
}

func (downloader Downloader) Download(ctx context.Context, rawURL, destination, expectedSHA256 string, policy Policy) (DownloadResult, error) {
	if ctx == nil || policy.MaxBytes < 1 || policy.ExpectedBytes < 0 || policy.ExpectedBytes > policy.MaxBytes || strings.TrimSpace(destination) == "" {
		return DownloadResult{}, errors.New("制品下载参数无效")
	}
	source, err := validateURL(rawURL, policy)
	if err != nil {
		return DownloadResult{}, err
	}
	expected, err := normalizeSHA256(expectedSHA256)
	if err != nil {
		return DownloadResult{}, err
	}
	client := downloader.secureClient(policy)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("创建制品下载请求: %w", err)
	}
	request.Header.Set("Accept", "application/octet-stream, application/gzip")
	if err := downloader.authorize(ctx, request); err != nil {
		return DownloadResult{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("下载制品: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DownloadResult{}, &HTTPError{StatusCode: response.StatusCode}
	}
	if _, err := validateURL(response.Request.URL.String(), policy); err != nil {
		return DownloadResult{}, err
	}
	if response.ContentLength > policy.MaxBytes {
		return DownloadResult{}, errors.New("制品大小超过安全上限")
	}
	if policy.ExpectedBytes > 0 && response.ContentLength > 0 && response.ContentLength != policy.ExpectedBytes {
		return DownloadResult{}, errors.New("制品大小与签名清单不一致")
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return DownloadResult{}, fmt.Errorf("创建制品目录: %w", err)
	}
	temporary, err := os.CreateTemp(parent, ".artifact-download-*")
	if err != nil {
		return DownloadResult{}, fmt.Errorf("创建制品临时文件: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return DownloadResult{}, err
	}
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(response.Body, policy.MaxBytes+1))
	if err != nil {
		return DownloadResult{}, fmt.Errorf("写入制品: %w", err)
	}
	if written > policy.MaxBytes {
		return DownloadResult{}, errors.New("制品大小超过安全上限")
	}
	if policy.ExpectedBytes > 0 && written != policy.ExpectedBytes {
		return DownloadResult{}, errors.New("制品大小与签名清单不一致")
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return DownloadResult{}, errors.New("制品 SHA-256 不匹配")
	}
	if err := temporary.Sync(); err != nil {
		return DownloadResult{}, fmt.Errorf("同步制品文件: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return DownloadResult{}, fmt.Errorf("关闭制品文件: %w", err)
	}
	if _, err := os.Lstat(destination); err == nil {
		return DownloadResult{}, errors.New("制品目标文件已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return DownloadResult{}, err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return DownloadResult{}, fmt.Errorf("发布制品文件: %w", err)
	}
	committed = true
	return DownloadResult{Bytes: written, SHA256: actual}, nil
}

// DownloadAndHash 按已知精确大小或未知大小上限下载，并返回本地计算的 SHA-256。
func (downloader Downloader) DownloadAndHash(ctx context.Context, rawURL, destination string, expectedBytes int64, policy Policy) (DownloadResult, error) {
	if ctx == nil || policy.MaxBytes < 1 || expectedBytes < 0 || expectedBytes > policy.MaxBytes || strings.TrimSpace(destination) == "" {
		return DownloadResult{}, errors.New("制品下载参数无效")
	}
	source, err := validateURL(rawURL, policy)
	if err != nil {
		return DownloadResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("创建制品下载请求: %w", err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	if err := downloader.authorize(ctx, request); err != nil {
		return DownloadResult{}, err
	}
	response, err := downloader.secureClient(policy).Do(request)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("下载制品: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DownloadResult{}, &HTTPError{StatusCode: response.StatusCode}
	}
	if _, err := validateURL(response.Request.URL.String(), policy); err != nil {
		return DownloadResult{}, err
	}
	if response.ContentLength > policy.MaxBytes || expectedBytes > 0 && response.ContentLength > 0 && response.ContentLength != expectedBytes {
		return DownloadResult{}, errors.New("制品大小与记录不一致")
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return DownloadResult{}, fmt.Errorf("创建制品目录: %w", err)
	}
	temporary, err := os.CreateTemp(parent, ".artifact-download-*")
	if err != nil {
		return DownloadResult{}, fmt.Errorf("创建制品临时文件: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return DownloadResult{}, err
	}
	hasher := sha256.New()
	limit := policy.MaxBytes
	if expectedBytes > 0 {
		limit = expectedBytes
	}
	written, err := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(response.Body, limit+1))
	if err != nil {
		return DownloadResult{}, fmt.Errorf("写入制品: %w", err)
	}
	if written < 1 || written > policy.MaxBytes || expectedBytes > 0 && written != expectedBytes {
		return DownloadResult{}, errors.New("制品大小与记录不一致")
	}
	if err := temporary.Sync(); err != nil {
		return DownloadResult{}, fmt.Errorf("同步制品文件: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return DownloadResult{}, fmt.Errorf("关闭制品文件: %w", err)
	}
	if _, err := os.Lstat(destination); err == nil {
		return DownloadResult{}, errors.New("制品目标文件已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return DownloadResult{}, err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return DownloadResult{}, fmt.Errorf("发布制品文件: %w", err)
	}
	committed = true
	return DownloadResult{Bytes: written, SHA256: hex.EncodeToString(hasher.Sum(nil))}, nil
}

func (downloader Downloader) authorize(ctx context.Context, request *http.Request) error {
	if downloader.Authorize == nil {
		return nil
	}
	if err := downloader.Authorize(ctx, request); err != nil {
		return fmt.Errorf("授权制品请求: %w", err)
	}
	return nil
}

func (downloader Downloader) secureClient(policy Policy) *http.Client {
	client := downloader.Client
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	previousRedirect := copyClient.CheckRedirect
	copyClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("制品下载重定向次数过多")
		}
		if _, err := validateURL(request.URL.String(), policy); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		return nil
	}
	return &copyClient
}

func ExtractTarGzip(source, destination string, policy Policy) (ExtractResult, error) {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(destination) == "" || policy.MaxEntries < 1 || policy.MaxExtractedBytes < 1 || policy.MaxPathBytes < 1 {
		return ExtractResult{}, errors.New("制品解压参数无效")
	}
	if _, err := os.Lstat(destination); err == nil {
		return ExtractResult{}, errors.New("制品解压目标已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ExtractResult{}, err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return ExtractResult{}, fmt.Errorf("创建制品解压目录: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.RemoveAll(destination)
		}
	}()
	file, err := os.Open(source)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("打开制品归档: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("读取 gzip 制品: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	result := ExtractResult{}
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return ExtractResult{}, fmt.Errorf("读取 tar 制品: %w", nextErr)
		}
		result.Entries++
		if result.Entries > policy.MaxEntries {
			return ExtractResult{}, errors.New("制品归档成员数量超过安全上限")
		}
		if isArchiveRootDirectory(header.Name, header.Typeflag) {
			continue
		}
		cleanName, err := safeArchivePath(header.Name, policy.MaxPathBytes)
		if err != nil {
			return ExtractResult{}, err
		}
		target := filepath.Join(destination, filepath.FromSlash(cleanName))
		if !isDescendant(target, destination) {
			return ExtractResult{}, errors.New("制品归档路径越界")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureDirectory(target, fileMode(header.Mode)); err != nil {
				return ExtractResult{}, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || result.Bytes+header.Size > policy.MaxExtractedBytes {
				return ExtractResult{}, errors.New("制品解压大小超过安全上限")
			}
			if err := ensureDirectory(filepath.Dir(target), 0o755); err != nil {
				return ExtractResult{}, err
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode(header.Mode))
			if err != nil {
				return ExtractResult{}, fmt.Errorf("创建解压文件: %w", err)
			}
			written, copyErr := io.CopyN(output, tarReader, header.Size)
			closeErr := output.Close()
			if copyErr != nil || closeErr != nil || written != header.Size {
				return ExtractResult{}, errors.Join(copyErr, closeErr, errors.New("制品成员写入不完整"))
			}
			result.Bytes += written
		default:
			return ExtractResult{}, errors.New("制品归档包含链接或特殊文件")
		}
	}
	succeeded = true
	return result, nil
}

func ExtractZIP(source, destination string, policy Policy) (ExtractResult, error) {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(destination) == "" || policy.MaxEntries < 1 || policy.MaxExtractedBytes < 1 || policy.MaxPathBytes < 1 {
		return ExtractResult{}, errors.New("ZIP 制品解压参数无效")
	}
	if _, err := os.Lstat(destination); err == nil {
		return ExtractResult{}, errors.New("ZIP 制品解压目标已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ExtractResult{}, err
	}
	reader, err := zip.OpenReader(source)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("打开 ZIP 制品: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > policy.MaxEntries {
		return ExtractResult{}, errors.New("ZIP 制品成员数量超过安全上限")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return ExtractResult{}, fmt.Errorf("创建 ZIP 解压目录: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.RemoveAll(destination)
		}
	}()
	result := ExtractResult{}
	for _, member := range reader.File {
		result.Entries++
		cleanName, pathErr := safeArchivePath(member.Name, policy.MaxPathBytes)
		if pathErr != nil {
			return ExtractResult{}, pathErr
		}
		target := filepath.Join(destination, filepath.FromSlash(cleanName))
		if !isDescendant(target, destination) {
			return ExtractResult{}, errors.New("ZIP 制品路径越界")
		}
		mode := member.Mode()
		if mode&os.ModeSymlink != 0 || mode&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket) != 0 {
			return ExtractResult{}, errors.New("ZIP 制品包含链接或特殊文件")
		}
		if member.FileInfo().IsDir() {
			if err := ensureDirectory(target, fileMode(int64(mode.Perm()))); err != nil {
				return ExtractResult{}, err
			}
			continue
		}
		if !mode.IsRegular() || member.UncompressedSize64 > uint64(policy.MaxExtractedBytes) || result.Bytes+int64(member.UncompressedSize64) > policy.MaxExtractedBytes {
			return ExtractResult{}, errors.New("ZIP 制品解压大小或文件类型无效")
		}
		if err := ensureDirectory(filepath.Dir(target), 0o755); err != nil {
			return ExtractResult{}, err
		}
		input, err := member.Open()
		if err != nil {
			return ExtractResult{}, fmt.Errorf("打开 ZIP 成员: %w", err)
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode(int64(mode.Perm())))
		if err != nil {
			input.Close()
			return ExtractResult{}, fmt.Errorf("创建 ZIP 解压文件: %w", err)
		}
		written, copyErr := io.Copy(output, io.LimitReader(input, int64(member.UncompressedSize64)+1))
		closeErr := errors.Join(output.Close(), input.Close())
		if copyErr != nil || closeErr != nil || written != int64(member.UncompressedSize64) {
			return ExtractResult{}, errors.Join(copyErr, closeErr, errors.New("ZIP 制品成员写入不完整"))
		}
		result.Bytes += written
	}
	succeeded = true
	return result, nil
}

// InspectArchive 在不发布解压目录的情况下验证归档结构、成员内容和安全上限。
func InspectArchive(source, format string, policy Policy) (ExtractResult, error) {
	if strings.TrimSpace(source) == "" || policy.MaxEntries < 1 || policy.MaxExtractedBytes < 1 || policy.MaxPathBytes < 1 {
		return ExtractResult{}, errors.New("制品归档检查参数无效")
	}
	switch format {
	case ".zip":
		return inspectZIP(source, policy)
	case ".tar.gz":
		return inspectTarGzip(source, policy)
	default:
		return ExtractResult{}, errors.New("制品归档格式不受支持")
	}
}

func inspectZIP(source string, policy Policy) (ExtractResult, error) {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("打开 ZIP 制品: %w", err)
	}
	defer reader.Close()
	if len(reader.File) < 1 || len(reader.File) > policy.MaxEntries {
		return ExtractResult{}, errors.New("ZIP 制品成员数量无效")
	}
	result := ExtractResult{}
	seen := make(map[string]struct{}, len(reader.File))
	for _, member := range reader.File {
		cleanName, err := safeArchivePath(member.Name, policy.MaxPathBytes)
		if err != nil {
			return ExtractResult{}, err
		}
		if _, duplicate := seen[cleanName]; duplicate {
			return ExtractResult{}, errors.New("ZIP 制品包含重复路径")
		}
		seen[cleanName] = struct{}{}
		result.Entries++
		mode := member.Mode()
		if mode&os.ModeSymlink != 0 || mode&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket) != 0 {
			return ExtractResult{}, errors.New("ZIP 制品包含链接或特殊文件")
		}
		if member.FileInfo().IsDir() {
			continue
		}
		if !mode.IsRegular() || member.UncompressedSize64 > uint64(policy.MaxExtractedBytes) ||
			result.Bytes+int64(member.UncompressedSize64) > policy.MaxExtractedBytes {
			return ExtractResult{}, errors.New("ZIP 制品解压大小或文件类型无效")
		}
		input, err := member.Open()
		if err != nil {
			return ExtractResult{}, fmt.Errorf("打开 ZIP 成员: %w", err)
		}
		written, copyErr := io.Copy(io.Discard, io.LimitReader(input, int64(member.UncompressedSize64)+1))
		closeErr := input.Close()
		if copyErr != nil || closeErr != nil || written != int64(member.UncompressedSize64) {
			return ExtractResult{}, errors.Join(copyErr, closeErr, errors.New("ZIP 制品成员内容无效"))
		}
		result.Bytes += written
	}
	return result, nil
}

func inspectTarGzip(source string, policy Policy) (ExtractResult, error) {
	file, err := os.Open(source)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("打开制品归档: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("读取 gzip 制品: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	result := ExtractResult{}
	seen := make(map[string]struct{})
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return ExtractResult{}, fmt.Errorf("读取 tar 制品: %w", nextErr)
		}
		result.Entries++
		if result.Entries > policy.MaxEntries {
			return ExtractResult{}, errors.New("制品归档成员数量超过安全上限")
		}
		if isArchiveRootDirectory(header.Name, header.Typeflag) {
			continue
		}
		cleanName, err := safeArchivePath(header.Name, policy.MaxPathBytes)
		if err != nil {
			return ExtractResult{}, err
		}
		if _, duplicate := seen[cleanName]; duplicate {
			return ExtractResult{}, errors.New("制品归档包含重复路径")
		}
		seen[cleanName] = struct{}{}
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || result.Bytes+header.Size > policy.MaxExtractedBytes {
				return ExtractResult{}, errors.New("制品解压大小超过安全上限")
			}
			written, copyErr := io.CopyN(io.Discard, tarReader, header.Size)
			if copyErr != nil || written != header.Size {
				return ExtractResult{}, errors.Join(copyErr, errors.New("制品成员内容无效"))
			}
			result.Bytes += written
		default:
			return ExtractResult{}, errors.New("制品归档包含链接或特殊文件")
		}
	}
	if result.Entries < 1 {
		return ExtractResult{}, errors.New("制品归档不能为空")
	}
	if _, err := io.Copy(io.Discard, gzipReader); err != nil {
		return ExtractResult{}, fmt.Errorf("验证 gzip 制品完整性: %w", err)
	}
	return result, nil
}

func isArchiveRootDirectory(name string, typeflag byte) bool {
	if typeflag != tar.TypeDir {
		return false
	}
	name = strings.TrimSpace(name)
	return name == "." || name == "./"
}

func validateURL(raw string, policy Policy) (*url.URL, error) {
	value, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || value.Scheme != "https" || value.Host == "" || value.User != nil || value.Fragment != "" {
		return nil, errors.New("制品 URL 必须是无凭据的 HTTPS 地址")
	}
	allowed := false
	for _, host := range policy.AllowedHosts {
		if strings.EqualFold(strings.TrimSpace(host), value.Host) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, errors.New("制品 URL host 不在白名单")
	}
	if !policyPathAllowed(value.EscapedPath(), policy) {
		return nil, errors.New("制品 URL 路径不在白名单")
	}
	return value, nil
}

func policyPathAllowed(value string, policy Policy) bool {
	prefixes := make([]string, 0, len(policy.PathPrefixes)+1)
	if prefix := strings.TrimSpace(policy.PathPrefix); prefix != "" {
		prefixes = append(prefixes, prefix)
	}
	for _, prefix := range policy.PathPrefixes {
		if prefix = strings.TrimSpace(prefix); prefix != "" {
			prefixes = append(prefixes, prefix)
		}
	}
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		if pathPrefixAllowed(value, prefix) {
			return true
		}
	}
	return false
}

func pathPrefixAllowed(value, prefix string) bool {
	if prefix == "/" {
		return strings.HasPrefix(value, "/")
	}
	if strings.HasSuffix(prefix, "/") {
		return strings.HasPrefix(value, prefix)
	}
	return value == prefix || strings.HasPrefix(value, prefix+"/")
}

func normalizeSHA256(value string) (string, error) {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "sha256:")
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("制品 SHA-256 无效")
	}
	return value, nil
}

func safeArchivePath(name string, maxBytes int) (string, error) {
	if name == "" || len([]byte(name)) > maxBytes || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", errors.New("制品归档路径无效")
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("制品归档路径越界")
	}
	return cleaned, nil
}

func isDescendant(candidate, root string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func ensureDirectory(target string, mode os.FileMode) error {
	if err := os.MkdirAll(target, mode); err != nil {
		return fmt.Errorf("创建解压目录: %w", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("制品解压目录类型无效")
	}
	return nil
}

func fileMode(raw int64) os.FileMode {
	mode := os.FileMode(raw) & 0o755
	if mode == 0 {
		return 0o600
	}
	return mode
}
