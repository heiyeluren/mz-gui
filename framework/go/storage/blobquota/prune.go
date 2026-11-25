package blobquota

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var ErrInvalidQuota = errors.New("缓存配额无效")

type Result struct {
	Removed int
	Kept    int
	Bytes   int64
}

type fileEntry struct {
	path string
	size int64
	mod  time.Time
}

func Prune(root string, maxBytes int64, maxAge time.Duration, now time.Time) (Result, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) || maxBytes < 1 || maxAge < 0 || now.IsZero() {
		return Result{}, ErrInvalidQuota
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Result{}, ErrInvalidQuota
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return Result{}, err
	}
	files := make([]fileEntry, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		stat, err := os.Lstat(path)
		if err != nil || !stat.Mode().IsRegular() {
			continue
		}
		files = append(files, fileEntry{path: path, size: stat.Size(), mod: stat.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].mod.Equal(files[j].mod) {
			return files[i].path < files[j].path
		}
		return files[i].mod.Before(files[j].mod)
	})
	var total int64
	for _, file := range files {
		total += file.size
	}
	removed := 0
	for _, file := range files {
		expired := maxAge > 0 && now.Sub(file.mod) > maxAge
		over := total > maxBytes
		if !expired && !over {
			break
		}
		if err := os.Remove(file.path); err != nil {
			return Result{}, err
		}
		total -= file.size
		removed++
	}
	return Result{Removed: removed, Kept: len(files) - removed, Bytes: total}, nil
}
