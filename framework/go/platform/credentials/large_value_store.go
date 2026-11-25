package credentials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ValueStore 是大值适配器所需的最小系统凭据库接口。
type ValueStore interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string) error
	Delete(context.Context, string) error
}

// MaxLargeValueBytes 是单个逻辑凭据的 UTF-8 字节上限。
const MaxLargeValueBytes = 256 << 10

const (
	largeValuePrefix        = "mz-large-value:"
	largeValueVersionPrefix = largeValuePrefix + "v1:"
	largeValueChunkPrefix   = "large-value-v1/"
	largeValueEntryBytes    = 2048
	largeValueChunkBytes    = largeValueEntryBytes / 4 * 3
	largeValueMaxParts      = (MaxLargeValueBytes + largeValueChunkBytes - 1) / largeValueChunkBytes
)

// 同进程内不同包装器也串行修改同一逻辑键。跨进程写入仍须由宿主协调；
// ValueStore 不提供跨进程 CAS，不能据此声称具备跨进程事务。
var largeValueLocks [64]sync.Mutex

// LargeValueStore 显式启用不可变分片，所有材料仍经调用方的系统凭据库保存。
// 原始记录可直接读取；新写入统一使用短清单，分片 Base64 后不超过 2048 字节。
// 覆盖失败可能已提交新值，调用方应 Get 回读。无法确定清单写入结果时保留
// 新分片，绝不回滚删除可能已被引用的数据；未提交孤片不自动枚举清理。
// 内部分片键和 mz-large-value: 清单前缀为本适配器保留。
type LargeValueStore struct{ store ValueStore }

func NewLargeValueStore(store ValueStore) (*LargeValueStore, error) {
	if store == nil {
		return nil, ErrInvalidArgument
	}
	return &LargeValueStore{store: store}, nil
}

type largeValueGeneration struct {
	ID    string `json:"id"`
	Parts int    `json:"parts"`
}

type largeValueManifest struct {
	Generation largeValueGeneration  `json:"generation"`
	Bytes      int                   `json:"bytes"`
	Digest     string                `json:"sha256"`
	Retired    *largeValueGeneration `json:"retired,omitempty"`
	Deleted    bool                  `json:"deleted,omitempty"`
}

func (s *LargeValueStore) Get(ctx context.Context, key string) (string, error) {
	if err := s.validate(ctx, key); err != nil {
		return "", err
	}
	lock := largeValueLock(key)
	lock.Lock()
	defer lock.Unlock()
	raw, err := s.get(ctx, key)
	if err != nil {
		return "", err
	}
	manifest, err := decodeLargeValueManifest(raw)
	if err != nil {
		return "", err
	}
	if manifest == nil {
		return raw, nil
	}
	if manifest.Deleted {
		return "", ErrNotFound
	}
	payload := make([]byte, 0, manifest.Bytes)
	for index := 0; index < manifest.Generation.Parts; index++ {
		part, err := s.get(ctx, largeValueChunkKey(key, manifest.Generation.ID, index))
		if err != nil {
			return "", largeValueReadError(err)
		}
		if len(part) > largeValueEntryBytes {
			return "", ErrUnavailable
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(part)
		remaining := manifest.Bytes - len(payload)
		expected := min(largeValueChunkBytes, remaining)
		if err != nil || len(decoded) != expected {
			return "", ErrUnavailable
		}
		payload = append(payload, decoded...)
	}
	digest := sha256.Sum256(payload)
	if len(payload) != manifest.Bytes || hex.EncodeToString(digest[:]) != manifest.Digest || !utf8.Valid(payload) {
		return "", ErrUnavailable
	}
	return string(payload), nil
}

func (s *LargeValueStore) Set(ctx context.Context, key, value string) error {
	if err := s.validate(ctx, key); err != nil {
		return err
	}
	if len(value) == 0 || len(value) > MaxLargeValueBytes || !utf8.ValidString(value) {
		return ErrInvalidArgument
	}
	lock := largeValueLock(key)
	lock.Lock()
	defer lock.Unlock()
	previous, err := s.readManifest(ctx, key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if previous != nil && previous.Retired != nil {
		if err := s.cleanup(ctx, key, *previous.Retired); err != nil {
			return err
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ErrUnavailable
	}
	digest := sha256.Sum256([]byte(value))
	manifest := largeValueManifest{
		Generation: largeValueGeneration{ID: hex.EncodeToString(nonce[:]), Parts: (len(value) + largeValueChunkBytes - 1) / largeValueChunkBytes},
		Bytes:      len(value), Digest: hex.EncodeToString(digest[:]),
	}
	if previous != nil {
		manifest.Retired = &previous.Generation
	}
	for index := 0; index < manifest.Generation.Parts; index++ {
		start := index * largeValueChunkBytes
		end := min(start+largeValueChunkBytes, len(value))
		part := base64.StdEncoding.EncodeToString([]byte(value[start:end]))
		if err := s.set(ctx, largeValueChunkKey(key, manifest.Generation.ID, index), part); err != nil {
			// 此时尚未尝试提交清单，连写入结果不明的当前片也绝不会被引用。
			s.cleanupUncommitted(key, largeValueGeneration{ID: manifest.Generation.ID, Parts: index + 1})
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		s.cleanupUncommitted(key, manifest.Generation)
		return err
	}
	if err := s.writeManifest(ctx, key, manifest); err != nil {
		// 底层可能已经提交，但返回错误；不能删除新分片或恢复旧清单。
		return err
	}
	if manifest.Retired != nil {
		// 新清单保留上代引用；清理中断后，下次 Set/Delete 可继续处理。
		return s.cleanup(ctx, key, *manifest.Retired)
	}
	return nil
}

func (s *LargeValueStore) Delete(ctx context.Context, key string) error {
	if err := s.validate(ctx, key); err != nil {
		return err
	}
	lock := largeValueLock(key)
	lock.Lock()
	defer lock.Unlock()
	manifest, err := s.readManifest(ctx, key)
	if err != nil {
		return err
	}
	if manifest == nil {
		return s.delete(ctx, key)
	}
	if !manifest.Deleted {
		manifest.Deleted = true
		if err := s.writeManifest(ctx, key, *manifest); err != nil {
			// 墓碑可能尚未提交，原值可能仍可见，保留全部分片。
			return err
		}
	}
	if err := s.cleanup(ctx, key, manifest.Generation); err != nil {
		return err
	}
	if manifest.Retired != nil {
		if err := s.cleanup(ctx, key, *manifest.Retired); err != nil {
			return err
		}
	}
	return s.delete(ctx, key)
}

func (s *LargeValueStore) validate(ctx context.Context, key string) error {
	if s == nil || s.store == nil || strings.HasPrefix(key, largeValueChunkPrefix) {
		return ErrInvalidArgument
	}
	return validateContextAndKey(ctx, key)
}

func largeValueLock(key string) *sync.Mutex {
	digest := sha256.Sum256([]byte(key))
	return &largeValueLocks[int(digest[0])%len(largeValueLocks)]
}

func largeValueChunkKey(key, generation string, index int) string {
	digest := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s%x/%s/%04x", largeValueChunkPrefix, digest, generation, index)
}

func (s *LargeValueStore) readManifest(ctx context.Context, key string) (*largeValueManifest, error) {
	raw, err := s.get(ctx, key)
	if err != nil {
		return nil, err
	}
	return decodeLargeValueManifest(raw)
}

func decodeLargeValueManifest(raw string) (*largeValueManifest, error) {
	if !strings.HasPrefix(raw, largeValuePrefix) {
		if raw == "" || len(raw) > MaxLargeValueBytes || !utf8.ValidString(raw) {
			return nil, ErrUnavailable
		}
		return nil, nil
	}
	if !strings.HasPrefix(raw, largeValueVersionPrefix) || len(raw) > largeValueEntryBytes {
		return nil, ErrUnavailable
	}
	var manifest largeValueManifest
	decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(raw, largeValueVersionPrefix)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrUnavailable
	}
	if manifest.Bytes < 1 || manifest.Bytes > MaxLargeValueBytes || !validLargeValueGeneration(manifest.Generation) ||
		manifest.Generation.Parts != (manifest.Bytes+largeValueChunkBytes-1)/largeValueChunkBytes || !largeValueHex(manifest.Digest, 64) {
		return nil, ErrUnavailable
	}
	if manifest.Retired != nil && (!validLargeValueGeneration(*manifest.Retired) || manifest.Retired.ID == manifest.Generation.ID) {
		return nil, ErrUnavailable
	}
	return &manifest, nil
}

func validLargeValueGeneration(g largeValueGeneration) bool {
	return largeValueHex(g.ID, 32) && g.Parts >= 1 && g.Parts <= largeValueMaxParts
}

func largeValueHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func (s *LargeValueStore) writeManifest(ctx context.Context, key string, manifest largeValueManifest) error {
	payload, err := json.Marshal(manifest)
	if err != nil {
		return ErrUnavailable
	}
	return s.set(ctx, key, largeValueVersionPrefix+string(payload))
}

func (s *LargeValueStore) get(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, err := s.store.Get(ctx, key)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return value, largeValueError(err)
}

func (s *LargeValueStore) set(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(value) > largeValueEntryBytes || len(key) > maxKeyLength {
		return ErrInvalidArgument
	}
	return largeValueError(s.store.Set(ctx, key, value))
}

func (s *LargeValueStore) delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return largeValueError(s.store.Delete(ctx, key))
}

func (s *LargeValueStore) cleanup(ctx context.Context, key string, generation largeValueGeneration) error {
	for index := 0; index < generation.Parts; index++ {
		err := s.delete(ctx, largeValueChunkKey(key, generation.ID, index))
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}

func (s *LargeValueStore) cleanupUncommitted(key string, generation largeValueGeneration) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.cleanup(ctx, key, generation)
}

func largeValueError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, ErrNotFound):
		return ErrNotFound
	case errors.Is(err, ErrInvalidArgument):
		return ErrInvalidArgument
	default:
		return ErrUnavailable
	}
}

func largeValueReadError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return largeValueError(err)
	}
	return ErrUnavailable
}
