package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// MaxChunkBytes 限制一次桥接请求的内存占用，不限制文件总大小。
const MaxChunkBytes = 256 * 1024
const streamDateLayout = "20060102-150405.000"

var mediaPrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func validMediaPrefix(value string) bool {
	return mediaPrefixPattern.MatchString(value)
}

func streamName(prefix string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + `-([0-9]{8}-[0-9]{6}\.[0-9]{3})-[0-9a-f]{32}\.(webm|m4a|ogg)$`)
}

func (store *Store) streamRecording(token string, startedMS int64, mime string) (Recording, error) {
	if len(token) != 32 {
		return Recording{}, errors.New("录音分片标识无效")
	}
	id, err := hex.DecodeString(token)
	mime = normalizeMIMEType(mime)
	format, ok := mediaFormats[mime]
	started := time.UnixMilli(startedMS).UTC()
	if err != nil || !ok || (mime != "audio/webm" && mime != "audio/mp4" && mime != "audio/ogg") || started.Year() < 1970 || started.Year() > 9999 {
		return Recording{}, errors.New("录音分片标识或格式无效")
	}
	// 文件名仅保存摘要，打开目录或读取历史不会授予继续写入、放弃录音的能力。
	digest := sha256.Sum256(id)
	name := fmt.Sprintf("%s-%s-%s%s", store.livePrefix, started.Format(streamDateLayout), hex.EncodeToString(digest[:16]), format.extension)
	return Recording{Filename: name, Path: filepath.Join(store.root, name), MIMEType: mime, CreatedAt: started}, nil
}

// Append 仅在预期位置顺序追加；同一分片重试会校验磁盘前缀，回执前同步文件。
func (store *Store) Append(token string, startedMS int64, mime string, offset int64, data []byte) (Recording, error) {
	rec, err := store.streamRecording(token, startedMS, mime)
	if err != nil {
		return Recording{}, err
	}
	if offset < 0 || len(data) == 0 || len(data) > MaxChunkBytes || (offset == 0 && !mediaFormats[rec.MIMEType].valid(data)) {
		return Recording{}, errors.New("录音分片内容无效")
	}
	store.streamMu.Lock()
	defer store.streamMu.Unlock()
	if err = store.Ensure(); err != nil {
		return Recording{}, err
	}
	info, err := os.Lstat(rec.Path)
	flags := os.O_RDWR
	if errors.Is(err, os.ErrNotExist) && offset == 0 {
		flags |= os.O_CREATE | os.O_EXCL
	} else if err != nil {
		return Recording{}, err
	} else if !info.Mode().IsRegular() {
		return Recording{}, errors.New("录音文件类型无效")
	}
	file, err := os.OpenFile(rec.Path, flags, 0600)
	if err != nil {
		return Recording{}, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return Recording{}, err
	}
	if info != nil && !os.SameFile(actual, info) {
		return Recording{}, errors.New("录音文件已替换")
	}
	overlap := actual.Size() - offset
	if overlap < 0 || overlap > int64(len(data)) {
		return Recording{}, errors.New("录音分片位置不连续")
	}
	if overlap > 0 {
		prefix := make([]byte, overlap)
		if _, err = file.ReadAt(prefix, offset); err != nil {
			return Recording{}, err
		}
		if !bytes.Equal(prefix, data[:overlap]) {
			return Recording{}, errors.New("录音分片与已保存内容冲突")
		}
	}
	if overlap < int64(len(data)) {
		if _, err = file.WriteAt(data[overlap:], actual.Size()); err != nil {
			return Recording{}, err
		}
	}
	if err = file.Sync(); err != nil {
		return Recording{}, err
	}
	rec.SizeBytes = offset + int64(len(data))
	return rec, nil
}

// Streams 从磁盘列出已落盘的录音，重启后不依赖前端历史或进程状态。
func (store *Store) Streams() ([]Recording, error) {
	if err := store.Ensure(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		return nil, err
	}
	recordings := []Recording{}
	namePattern := streamName(store.livePrefix)
	for _, entry := range entries {
		match := namePattern.FindStringSubmatch(entry.Name())
		if match == nil || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Size() == 0 {
			continue
		}
		created, err := time.Parse(streamDateLayout, match[1])
		if err != nil {
			continue
		}
		mime := "audio/" + match[2]
		if match[2] == "m4a" {
			mime = "audio/mp4"
		}
		recordings = append(recordings, Recording{Filename: entry.Name(), Path: filepath.Join(store.root, entry.Name()), MIMEType: mime, SizeBytes: info.Size(), CreatedAt: created})
	}
	return recordings, nil
}

// DiscardStream 只响应用户显式放弃当前录音；页面退出不走这里。
func (store *Store) DiscardStream(token string, startedMS int64, mime string) error {
	rec, err := store.streamRecording(token, startedMS, mime)
	if err != nil {
		return err
	}
	store.streamMu.Lock()
	defer store.streamMu.Unlock()
	if err = store.Ensure(); err != nil {
		return err
	}
	err = os.Remove(rec.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
