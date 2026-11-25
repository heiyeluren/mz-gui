package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const DefaultMaxRecordingBytes int64 = 32 * 1024 * 1024

type Options struct {
	Root        string
	MaxBytes    int64
	Now         func() time.Time
	Random      io.Reader
	VoicePrefix string
	LivePrefix  string
}

type Recording struct {
	Filename   string
	Path       string
	MIMEType   string
	SizeBytes  int64
	DurationMS int64
	CreatedAt  time.Time
}

type Store struct {
	streamMu    sync.Mutex
	root        string
	maxBytes    int64
	now         func() time.Time
	random      io.Reader
	voicePrefix string
	livePrefix  string
}

type mediaFormat struct {
	extension string
	valid     func([]byte) bool
}

var mediaFormats = map[string]mediaFormat{
	"audio/webm": {extension: ".webm", valid: func(data []byte) bool {
		return len(data) >= 4 && data[0] == 0x1a && data[1] == 0x45 && data[2] == 0xdf && data[3] == 0xa3
	}},
	"audio/ogg": {extension: ".ogg", valid: func(data []byte) bool {
		return len(data) >= 4 && string(data[:4]) == "OggS"
	}},
	"audio/wav": {extension: ".wav", valid: func(data []byte) bool {
		return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE"
	}},
	"audio/x-wav": {extension: ".wav", valid: func(data []byte) bool {
		return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE"
	}},
	"audio/mp4": {extension: ".m4a", valid: func(data []byte) bool {
		return len(data) >= 12 && string(data[4:8]) == "ftyp"
	}},
}

func NewStore(options Options) (*Store, error) {
	root := filepath.Clean(strings.TrimSpace(options.Root))
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("媒体存储目录必须是绝对路径")
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = DefaultMaxRecordingBytes
	}
	if options.MaxBytes < 1 || options.MaxBytes > DefaultMaxRecordingBytes {
		return nil, errors.New("媒体存储容量限制无效")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.VoicePrefix == "" {
		options.VoicePrefix = "recording"
	}
	if options.LivePrefix == "" {
		options.LivePrefix = "recording-live"
	}
	if !validMediaPrefix(options.VoicePrefix) || !validMediaPrefix(options.LivePrefix) {
		return nil, errors.New("录音文件名前缀无效")
	}
	return &Store{root: root, maxBytes: options.MaxBytes, now: options.Now, random: options.Random, voicePrefix: options.VoicePrefix, livePrefix: options.LivePrefix}, nil
}

func (store *Store) Ensure() error {
	return ensurePrivateDirectory(store.root)
}

func (store *Store) Save(ctx context.Context, rawMIMEType string, data []byte, duration time.Duration) (Recording, error) {
	if ctx == nil || ctx.Err() != nil {
		return Recording{}, errors.New("媒体保存上下文无效")
	}
	mimeType := normalizeMIMEType(rawMIMEType)
	format, found := mediaFormats[mimeType]
	if !found {
		return Recording{}, fmt.Errorf("不支持的录音格式 %q", mimeType)
	}
	if len(data) == 0 || int64(len(data)) > store.maxBytes {
		return Recording{}, errors.New("录音文件大小无效")
	}
	if duration <= 0 {
		return Recording{}, errors.New("录音时长无效")
	}
	if !format.valid(data) {
		return Recording{}, errors.New("录音文件头与声明格式不一致")
	}
	if err := ensurePrivateDirectory(store.root); err != nil {
		return Recording{}, err
	}

	createdAt := store.now().UTC()
	randomBytes := make([]byte, 8)
	if _, err := io.ReadFull(store.random, randomBytes); err != nil {
		return Recording{}, fmt.Errorf("生成录音文件标识: %w", err)
	}
	filename := fmt.Sprintf("%s-%s-%s%s", store.voicePrefix, createdAt.Format("20060102-150405"), hex.EncodeToString(randomBytes), format.extension)
	destination := filepath.Join(store.root, filename)
	temporary := filepath.Join(store.root, "."+filename+".part")
	if err := writeAtomic(ctx, temporary, destination, data); err != nil {
		return Recording{}, err
	}
	return Recording{
		Filename: filename, Path: destination, MIMEType: mimeType, SizeBytes: int64(len(data)),
		DurationMS: duration.Milliseconds(), CreatedAt: createdAt,
	}, nil
}

func normalizeMIMEType(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
}

func ensurePrivateDirectory(root string) error {
	info, err := os.Lstat(root)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("媒体存储目录不可信")
		}
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(root, 0o700); err != nil {
			return fmt.Errorf("创建媒体存储目录: %w", err)
		}
		info, err = os.Lstat(root)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("媒体存储目录不可信")
		}
	case err != nil:
		return fmt.Errorf("检查媒体存储目录: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return fmt.Errorf("设置媒体存储目录权限: %w", err)
	}
	return nil
}

func writeAtomic(ctx context.Context, temporary, destination string, data []byte) (resultErr error) {
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("创建录音临时文件: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temporary)
		}
	}()
	if ctx.Err() != nil {
		_ = file.Close()
		return ctx.Err()
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("写入录音文件: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("同步录音文件: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭录音文件: %w", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return errors.New("录音目标文件已存在")
	}
	if err := os.Rename(temporary, destination); err != nil {
		return fmt.Errorf("提交录音文件: %w", err)
	}
	return nil
}
