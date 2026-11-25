package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidOutbox = errors.New("语义事件持久队列配置无效")
	ErrOutboxFull    = errors.New("语义事件持久队列已满")
)

type OutboxOptions struct {
	Directory     string
	MaxEvents     int
	MaxEventBytes int64
	Now           func() time.Time
}

type OutboxRecord struct {
	Event       Event     `json:"event"`
	EnqueuedAt  time.Time `json:"enqueued_at"`
	Attempts    int       `json:"attempts"`
	NextRetryAt time.Time `json:"next_retry_at,omitempty"`
}

type DurableOutbox struct {
	mu      sync.Mutex
	options OutboxOptions
}

func NewDurableOutbox(options OutboxOptions) (*DurableOutbox, error) {
	directory := filepath.Clean(strings.TrimSpace(options.Directory))
	if directory == "" || !filepath.IsAbs(directory) || options.MaxEvents < 1 || options.MaxEvents > 10000 ||
		options.MaxEventBytes < 512 || options.MaxEventBytes > 1<<20 {
		return nil, ErrInvalidOutbox
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	options.Directory = directory
	if err := ensurePrivateOutboxDirectory(directory); err != nil {
		return nil, err
	}
	return &DurableOutbox{options: options}, nil
}

func (outbox *DurableOutbox) Enqueue(ctx context.Context, event Event) (OutboxRecord, error) {
	if ctx == nil || outbox == nil {
		return OutboxRecord{}, ErrInvalidOutbox
	}
	if err := ctx.Err(); err != nil {
		return OutboxRecord{}, err
	}
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	now := outbox.options.Now().UTC()
	normalized, err := Normalize(event, now)
	if err != nil {
		return OutboxRecord{}, err
	}
	if existing, exists, err := outbox.findByEventID(ctx, normalized.ID); err != nil || exists {
		return existing, err
	}
	files, err := outbox.files()
	if err != nil {
		return OutboxRecord{}, err
	}
	if len(files) >= outbox.options.MaxEvents {
		return OutboxRecord{}, ErrOutboxFull
	}
	record := OutboxRecord{Event: normalized, EnqueuedAt: now}
	if err := outbox.writeRecord(record); err != nil {
		return OutboxRecord{}, err
	}
	return record, nil
}

func (outbox *DurableOutbox) PeekReady(ctx context.Context, limit int) ([]OutboxRecord, error) {
	if ctx == nil || outbox == nil || limit < 1 || limit > MaxBatchSize {
		return nil, ErrInvalidOutbox
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	now := outbox.options.Now().UTC()
	records, err := outbox.readAll(ctx)
	if err != nil {
		return nil, err
	}
	ready := make([]OutboxRecord, 0, min(limit, len(records)))
	for _, record := range records {
		if record.NextRetryAt.IsZero() || !record.NextRetryAt.After(now) {
			ready = append(ready, record)
			if len(ready) >= limit {
				break
			}
		}
	}
	return ready, nil
}

func (outbox *DurableOutbox) MarkRetry(ctx context.Context, eventIDs []string, delay time.Duration) error {
	if ctx == nil || outbox == nil || len(eventIDs) == 0 || delay <= 0 || delay > 24*time.Hour {
		return ErrInvalidOutbox
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	now := outbox.options.Now().UTC()
	wanted := outbox.eventIDSet(eventIDs)
	files, err := outbox.files()
	if err != nil {
		return err
	}
	for _, path := range files {
		record, err := readOutboxRecord(path)
		if err != nil || !wanted[record.Event.ID] {
			continue
		}
		record.Attempts++
		record.NextRetryAt = now.Add(delay).UTC()
		if err := outbox.replaceRecord(path, record); err != nil {
			return err
		}
	}
	return nil
}

func (outbox *DurableOutbox) Ack(ctx context.Context, eventIDs []string) error {
	if ctx == nil || outbox == nil || len(eventIDs) == 0 {
		return ErrInvalidOutbox
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	wanted := outbox.eventIDSet(eventIDs)
	files, err := outbox.files()
	if err != nil {
		return err
	}
	for _, path := range files {
		record, err := readOutboxRecord(path)
		if err != nil || !wanted[record.Event.ID] {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("确认删除语义事件: %w", err)
		}
	}
	return nil
}

func (outbox *DurableOutbox) Clear(ctx context.Context) (int, error) {
	if ctx == nil || outbox == nil {
		return 0, ErrInvalidOutbox
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	files, err := outbox.files()
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("清空语义事件持久队列: %w", err)
		}
		removed++
	}
	return removed, nil
}

func (outbox *DurableOutbox) Len(ctx context.Context) (int, error) {
	if ctx == nil || outbox == nil {
		return 0, ErrInvalidOutbox
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	files, err := outbox.files()
	if err != nil {
		return 0, err
	}
	return len(files), nil
}

func (outbox *DurableOutbox) readAll(ctx context.Context) ([]OutboxRecord, error) {
	files, err := outbox.files()
	if err != nil {
		return nil, err
	}
	records := make([]OutboxRecord, 0, len(files))
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := readOutboxRecord(path)
		if err != nil {
			continue
		}
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].EnqueuedAt.Equal(records[j].EnqueuedAt) {
			return records[i].Event.ID < records[j].Event.ID
		}
		return records[i].EnqueuedAt.Before(records[j].EnqueuedAt)
	})
	return records, nil
}

func (outbox *DurableOutbox) findByEventID(ctx context.Context, eventID string) (OutboxRecord, bool, error) {
	records, err := outbox.readAll(ctx)
	if err != nil {
		return OutboxRecord{}, false, err
	}
	for _, record := range records {
		if record.Event.ID == eventID {
			return record, true, nil
		}
	}
	return OutboxRecord{}, false, nil
}

func (outbox *DurableOutbox) writeRecord(record OutboxRecord) error {
	path := filepath.Join(outbox.options.Directory, outboxRecordFileName(record))
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("检查语义事件持久记录: %w", err)
	}
	return writeOutboxRecord(path, record, outbox.options.MaxEventBytes, false)
}

func (outbox *DurableOutbox) replaceRecord(path string, record OutboxRecord) error {
	if filepath.Dir(filepath.Clean(path)) != outbox.options.Directory {
		return ErrInvalidOutbox
	}
	return writeOutboxRecord(path, record, outbox.options.MaxEventBytes, true)
}

func (outbox *DurableOutbox) eventIDSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if eventIDPattern.MatchString(value) {
			result[value] = true
		}
	}
	return result
}

func (outbox *DurableOutbox) files() ([]string, error) {
	if err := ensurePrivateOutboxDirectory(outbox.options.Directory); err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(outbox.options.Directory, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("扫描语义事件持久队列: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func readOutboxRecord(path string) (OutboxRecord, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return OutboxRecord{}, ErrInvalidOutbox
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return OutboxRecord{}, err
	}
	var record OutboxRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return OutboxRecord{}, err
	}
	if record.Event.ID == "" || record.Event.Name == "" || record.Event.Source == "" || record.Event.OccurredAt.IsZero() || record.EnqueuedAt.IsZero() {
		return OutboxRecord{}, ErrInvalidOutbox
	}
	return record, nil
}

func writeOutboxRecord(path string, record OutboxRecord, maxBytes int64, replace bool) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("编码语义事件持久记录: %w", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("%w: 语义事件持久记录超过上限", ErrInvalidOutbox)
	}
	if err := ensurePrivateOutboxDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".mz-gui-telemetry-*.tmp")
	if err != nil {
		return fmt.Errorf("创建语义事件持久记录临时文件: %w", err)
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
		return fmt.Errorf("设置语义事件持久记录权限: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("写入语义事件持久记录: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("同步语义事件持久记录: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭语义事件持久记录: %w", err)
	}
	if replace {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("替换语义事件持久记录: %w", err)
		}
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("提交语义事件持久记录: %w", err)
	}
	committed = true
	return nil
}

func outboxRecordFileName(record OutboxRecord) string {
	sum := sha256.Sum256([]byte(record.Event.ID))
	return fmt.Sprintf("%s-%s.json", record.EnqueuedAt.UTC().Format("20060102T150405.000000000Z"), hex.EncodeToString(sum[:8]))
}

func ensurePrivateOutboxDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("创建语义事件持久队列目录: %w", err)
		}
		return os.Chmod(directory, 0o700)
	}
	if err != nil {
		return fmt.Errorf("检查语义事件持久队列目录: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrInvalidOutbox
	}
	return os.Chmod(directory, 0o700)
}
