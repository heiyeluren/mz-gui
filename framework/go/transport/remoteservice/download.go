package remoteservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DownloaderOptions struct {
	Client *Client
}

type Downloader struct {
	client *Client
}

type DownloadRequest struct {
	URL            string
	Headers        http.Header
	TargetPath     string
	TempPath       string
	ExpectedSHA256 string
	ETag           string
	LastModified   string
	RequestID      string
	Timeout        time.Duration
	MaxBytes       int64
}

type DownloadResult struct {
	TargetPath   string
	TempPath     string
	BytesWritten int64
	SHA256       string
	Resumed      bool
	StatusCode   int
}

func NewDownloader(options DownloaderOptions) *Downloader {
	return &Downloader{client: options.Client}
}

func (downloader *Downloader) Download(ctx context.Context, request DownloadRequest) (DownloadResult, error) {
	if downloader == nil || downloader.client == nil || ctx == nil || ctx.Err() != nil {
		return DownloadResult{}, ErrInvalidRequest
	}
	if strings.TrimSpace(request.URL) == "" || strings.TrimSpace(request.TargetPath) == "" || strings.TrimSpace(request.TempPath) == "" {
		return DownloadResult{}, ErrInvalidRequest
	}
	if request.TargetPath == request.TempPath {
		return DownloadResult{}, ErrInvalidRequest
	}
	completed, err := tempFileSize(request.TempPath)
	if err != nil {
		return DownloadResult{}, err
	}
	maxBytes := request.MaxBytes
	if maxBytes <= 0 {
		maxBytes = downloader.client.policy.MaxBodyBytes
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBodyBytes
	}
	if completed > maxBytes {
		return DownloadResult{}, ErrBodyTooLarge
	}
	headers := cloneHeader(request.Headers)
	if completed > 0 {
		headers = TransferCheckpoint{
			URL: request.URL, CompletedBytes: completed, ETag: request.ETag, LastModified: request.LastModified,
		}.ApplyResumeHeaders(headers)
	}
	if err := ensureParentDir(request.TempPath); err != nil {
		return DownloadResult{}, err
	}
	var result DownloadResult
	execute := func(callContext context.Context) error {
		downloaded, status, err := downloader.downloadOnce(callContext, request, headers, completed, maxBytes)
		if err != nil {
			return err
		}
		hash, totalBytes, err := hashFile(request.TempPath)
		if err != nil {
			return err
		}
		if request.ExpectedSHA256 != "" && !strings.EqualFold(hash, request.ExpectedSHA256) {
			return ErrChecksumMismatch
		}
		if err := ensureParentDir(request.TargetPath); err != nil {
			return err
		}
		if err := os.Rename(request.TempPath, request.TargetPath); err != nil {
			return fmt.Errorf("提交 RemoteService 下载文件: %w", err)
		}
		result = DownloadResult{
			TargetPath: request.TargetPath, TempPath: request.TempPath, BytesWritten: totalBytes,
			SHA256: hash, Resumed: completed > 0, StatusCode: status,
		}
		_ = downloaded
		return nil
	}
	if downloader.client.limiter != nil {
		if err := downloader.client.limiter.Wait(ctx, 1); err != nil {
			return DownloadResult{}, err
		}
	}
	if downloader.client.breaker != nil {
		err = downloader.client.breaker.Execute(ctx, execute)
	} else {
		err = execute(ctx)
	}
	if err != nil {
		return DownloadResult{}, err
	}
	return result, nil
}

func tempFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return 0, ErrInvalidRequest
		}
		return info.Size(), nil
	}
	if os.IsNotExist(err) {
		return 0, nil
	}
	return 0, fmt.Errorf("读取 RemoteService 下载临时文件: %w", err)
}

func ensureParentDir(path string) error {
	parent := filepath.Dir(path)
	if parent == "." || parent == "" {
		return nil
	}
	return os.MkdirAll(parent, 0o700)
}

func (downloader *Downloader) downloadOnce(ctx context.Context, request DownloadRequest, headers http.Header, completed int64, maxBytes int64) (int64, int, error) {
	parsed, err := downloader.client.policy.Validate(http.MethodGet, request.URL, headers, 0)
	if err != nil {
		return 0, 0, err
	}
	callContext := ctx
	var cancel context.CancelFunc
	if request.Timeout > 0 {
		callContext, cancel = context.WithTimeout(ctx, request.Timeout)
		defer cancel()
	}
	httpRequest, err := http.NewRequestWithContext(callContext, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return 0, 0, err
	}
	for name, values := range headers {
		for _, value := range values {
			httpRequest.Header.Add(name, value)
		}
	}
	if request.RequestID != "" {
		httpRequest.Header.Set("X-Request-ID", request.RequestID)
	}
	if downloader.client.userAgent != "" && httpRequest.Header.Get("User-Agent") == "" {
		httpRequest.Header.Set("User-Agent", downloader.client.userAgent)
	}
	response, err := downloader.client.httpClient.Do(httpRequest)
	if err != nil {
		return 0, 0, err
	}
	defer response.Body.Close()
	if completed > 0 && response.StatusCode != http.StatusPartialContent {
		return 0, response.StatusCode, ErrInvalidResumeResponse
	}
	if completed == 0 && response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return 0, response.StatusCode, fmt.Errorf("%w: status=%d", ErrInvalidRequest, response.StatusCode)
	}
	written, err := appendToTemp(request.TempPath, response.Body, completed > 0, maxBytes-completed)
	if err != nil {
		return written, response.StatusCode, err
	}
	return written, response.StatusCode, nil
}

func appendToTemp(path string, reader io.Reader, appendMode bool, maxAppendBytes int64) (int64, error) {
	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return 0, fmt.Errorf("打开 RemoteService 下载临时文件: %w", err)
	}
	defer file.Close()
	if maxAppendBytes < 0 {
		return 0, ErrBodyTooLarge
	}
	limited := io.LimitReader(reader, maxAppendBytes+1)
	written, err := io.Copy(file, limited)
	if err != nil {
		return written, fmt.Errorf("写入 RemoteService 下载临时文件: %w", err)
	}
	if written > maxAppendBytes {
		return written, ErrBodyTooLarge
	}
	return written, nil
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("打开 RemoteService 下载文件校验: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, fmt.Errorf("校验 RemoteService 下载文件: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}
