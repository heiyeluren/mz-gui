package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DrillMarkerName = ".mzgui-uninstall-drill"

	UserDataConfig      = "config"
	UserDataDatabase    = "database"
	UserDataCache       = "cache"
	UserDataLogs        = "logs"
	UserDataCredentials = "credentials"
	UserDataStartup     = "startup"
	UserDataProtocol    = "protocol"

	ItemRemoved  = "removed"
	ItemKept     = "kept"
	ItemAbsent   = "absent"
	ItemFailed   = "failed"
	ItemRestored = "restored"
)

var (
	ErrInvalidUninstall    = errors.New("卸载参数无效")
	ErrUninstallRefused    = errors.New("拒绝卸载非演练安装根")
	ErrUninstallRolledBack = errors.New("卸载已回滚用户数据")
)

type ProcessTarget struct {
	Component string
	Path      string
}

type UserDataItem struct {
	Kind string
	Path string
}

type UninstallItemResult struct {
	Kind   string
	Path   string
	Status string
}

type UninstallResult struct {
	ProcessArtifacts []UninstallItemResult
	UserData         []UninstallItemResult
	KeptUserData     bool
}

type UninstallOptions struct {
	DrillRoot      string
	InstallRoot    string
	Components     []ProcessTarget
	UserData       []UserDataItem
	DeleteUserData bool
	Remove         func(string) error
	Quarantine     func(string, string) error
}

type stagedUserData struct {
	item UserDataItem
	dest string
}

// Uninstall 仅在带演练标记的 fake 安装根上移除进程制品；默认保留用户数据，可选删除并给出逐项结果。
func Uninstall(ctx context.Context, options UninstallOptions) (UninstallResult, error) {
	prepared, err := prepareUninstall(ctx, options)
	if err != nil {
		return UninstallResult{}, err
	}
	remove := options.Remove
	if remove == nil {
		remove = os.Remove
	}
	result := UninstallResult{KeptUserData: true}
	for _, target := range prepared.components {
		item, removeErr := removeProcessTarget(target, remove)
		result.ProcessArtifacts = append(result.ProcessArtifacts, item)
		if removeErr != nil {
			result.UserData = keepUserData(prepared.userData)
			return result, removeErr
		}
	}
	if !options.DeleteUserData {
		result.UserData = keepUserData(prepared.userData)
		return result, nil
	}
	data, err := deleteUserData(prepared.drillRoot, prepared.userData, options.Quarantine)
	result.UserData = data
	if err != nil {
		result.KeptUserData = true
		return result, err
	}
	result.KeptUserData = false
	return result, nil
}

type preparedUninstall struct {
	drillRoot  string
	components []ProcessTarget
	userData   []UserDataItem
}

func prepareUninstall(ctx context.Context, options UninstallOptions) (preparedUninstall, error) {
	if ctx == nil {
		return preparedUninstall{}, ErrInvalidUninstall
	}
	if err := ctx.Err(); err != nil {
		return preparedUninstall{}, err
	}
	drillRoot, err := requireDrillRoot(options.DrillRoot)
	if err != nil {
		return preparedUninstall{}, err
	}
	installRoot, err := requireInstallRoot(drillRoot, options.InstallRoot)
	if err != nil {
		return preparedUninstall{}, err
	}
	if len(options.Components) == 0 {
		return preparedUninstall{}, ErrInvalidUninstall
	}
	components := make([]ProcessTarget, 0, len(options.Components))
	seenComponent := make(map[string]struct{}, len(options.Components))
	for _, target := range options.Components {
		name := strings.TrimSpace(target.Component)
		if name == "" || !validProcessComponent(name) {
			return preparedUninstall{}, ErrInvalidUninstall
		}
		if _, exists := seenComponent[name]; exists {
			return preparedUninstall{}, ErrInvalidUninstall
		}
		path, err := requireContainedRegularFile(installRoot, target.Path)
		if err != nil {
			return preparedUninstall{}, err
		}
		seenComponent[name] = struct{}{}
		components = append(components, ProcessTarget{Component: name, Path: path})
	}
	userData := make([]UserDataItem, 0, len(options.UserData))
	seenKind := make(map[string]struct{}, len(options.UserData))
	for _, item := range options.UserData {
		kind := strings.TrimSpace(item.Kind)
		if !validUserDataKind(kind) {
			return preparedUninstall{}, ErrInvalidUninstall
		}
		if _, exists := seenKind[kind]; exists {
			return preparedUninstall{}, ErrInvalidUninstall
		}
		path, err := requireContainedPath(drillRoot, item.Path)
		if err != nil {
			return preparedUninstall{}, err
		}
		if path == filepath.Join(drillRoot, DrillMarkerName) {
			return preparedUninstall{}, ErrInvalidUninstall
		}
		seenKind[kind] = struct{}{}
		userData = append(userData, UserDataItem{Kind: kind, Path: path})
	}
	return preparedUninstall{drillRoot: drillRoot, components: components, userData: userData}, nil
}

func requireDrillRoot(raw string) (string, error) {
	root := filepath.Clean(strings.TrimSpace(raw))
	if root == "" || !filepath.IsAbs(root) || isForbiddenSystemRoot(root) {
		return "", ErrUninstallRefused
	}
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", ErrUninstallRefused
	}
	marker := filepath.Join(root, DrillMarkerName)
	markerInfo, err := os.Lstat(marker)
	if err != nil || markerInfo.Mode()&os.ModeSymlink != 0 || !markerInfo.Mode().IsRegular() {
		return "", ErrUninstallRefused
	}
	return root, nil
}

func requireInstallRoot(drillRoot, raw string) (string, error) {
	root := filepath.Clean(strings.TrimSpace(raw))
	if err := containPath(drillRoot, root); err != nil {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", ErrInvalidUninstall
	}
	return root, nil
}

func requireContainedRegularFile(root, raw string) (string, error) {
	path, err := requireContainedPath(root, raw)
	if err != nil {
		return "", err
	}
	if _, err := absRegularFile(path); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidUninstall, err)
	}
	return path, nil
}

func requireContainedPath(root, raw string) (string, error) {
	path := filepath.Clean(strings.TrimSpace(raw))
	if err := containPath(root, path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidUninstall, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalidUninstall
	}
	return path, nil
}

func containPath(root, path string) error {
	if path == "" || !filepath.IsAbs(path) || isForbiddenSystemRoot(path) {
		return ErrUninstallRefused
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrUninstallRefused
	}
	return nil
}

func isForbiddenSystemRoot(path string) bool {
	cleaned := filepath.Clean(path)
	if cleaned == string(filepath.Separator) {
		return true
	}
	for _, prefix := range []string{"/Applications", "/System"} {
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func validUserDataKind(value string) bool {
	switch value {
	case UserDataConfig, UserDataDatabase, UserDataCache, UserDataLogs, UserDataCredentials, UserDataStartup, UserDataProtocol:
		return true
	default:
		return false
	}
}

func removeProcessTarget(target ProcessTarget, remove func(string) error) (UninstallItemResult, error) {
	item := UninstallItemResult{Kind: target.Component, Path: target.Path}
	if _, err := os.Lstat(target.Path); errors.Is(err, os.ErrNotExist) {
		item.Status = ItemAbsent
		return item, nil
	} else if err != nil {
		item.Status = ItemFailed
		return item, fmt.Errorf("%w: %v", ErrInvalidUninstall, err)
	}
	if err := remove(target.Path); err != nil {
		item.Status = ItemFailed
		return item, fmt.Errorf("移除进程制品 %s: %w", target.Component, err)
	}
	item.Status = ItemRemoved
	return item, nil
}

func keepUserData(items []UserDataItem) []UninstallItemResult {
	result := make([]UninstallItemResult, 0, len(items))
	for _, item := range items {
		status := ItemKept
		if _, err := os.Lstat(item.Path); errors.Is(err, os.ErrNotExist) {
			status = ItemAbsent
		}
		result = append(result, UninstallItemResult{Kind: item.Kind, Path: item.Path, Status: status})
	}
	return result
}

func deleteUserData(drillRoot string, items []UserDataItem, quarantine func(string, string) error) ([]UninstallItemResult, error) {
	if quarantine == nil {
		quarantine = os.Rename
	}
	quarantineRoot := filepath.Join(drillRoot, ".uninstall-quarantine")
	if err := os.MkdirAll(quarantineRoot, 0o700); err != nil {
		return keepUserData(items), fmt.Errorf("%w: %v", ErrInvalidUninstall, err)
	}
	staged := make([]stagedUserData, 0, len(items))
	results := make([]UninstallItemResult, 0, len(items))
	for _, item := range items {
		entry := UninstallItemResult{Kind: item.Kind, Path: item.Path}
		if _, err := os.Lstat(item.Path); errors.Is(err, os.ErrNotExist) {
			entry.Status = ItemAbsent
			results = append(results, entry)
			continue
		}
		dest := filepath.Join(quarantineRoot, item.Kind)
		if err := quarantine(item.Path, dest); err != nil {
			entry.Status = ItemFailed
			results = append(results, entry)
			return restoreUserData(items, staged, results), fmt.Errorf("%w: %v", ErrUninstallRolledBack, err)
		}
		staged = append(staged, stagedUserData{item: item, dest: dest})
		entry.Status = ItemRemoved
		results = append(results, entry)
	}
	_ = os.RemoveAll(quarantineRoot)
	return results, nil
}

func restoreUserData(items []UserDataItem, staged []stagedUserData, current []UninstallItemResult) []UninstallItemResult {
	restored := make(map[string]struct{}, len(staged))
	for i := len(staged) - 1; i >= 0; i-- {
		item := staged[i]
		if err := os.MkdirAll(filepath.Dir(item.item.Path), 0o700); err != nil {
			continue
		}
		if err := os.Rename(item.dest, item.item.Path); err == nil {
			restored[item.item.Kind] = struct{}{}
		}
	}
	byKind := make(map[string]UninstallItemResult, len(current))
	for _, item := range current {
		if _, ok := restored[item.Kind]; ok {
			item.Status = ItemRestored
		}
		byKind[item.Kind] = item
	}
	result := make([]UninstallItemResult, 0, len(items))
	for _, item := range items {
		if entry, ok := byKind[item.Kind]; ok {
			result = append(result, entry)
			continue
		}
		status := ItemKept
		if _, err := os.Lstat(item.Path); errors.Is(err, os.ErrNotExist) {
			status = ItemAbsent
		}
		result = append(result, UninstallItemResult{Kind: item.Kind, Path: item.Path, Status: status})
	}
	return result
}
