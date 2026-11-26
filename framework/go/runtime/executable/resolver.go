// Package executable 提供不依赖交互式 shell 的受控可执行文件解析能力。
package executable

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	ErrInvalidOptions = errors.New("可执行文件解析配置无效")
	ErrInvalidName    = errors.New("可执行文件名称无效")
	ErrNotFound       = errors.New("可执行文件不存在")
)

type Options struct {
	GOOS                     string
	SearchPath               string
	PATHEXT                  string
	TrustedDirectories       []string
	TrustedTargetDirectories []string
	// 用户显式提供的 PATH 可优先于宿主兼容目录；默认保持原有受管策略。
	SearchPathFirst bool
	// 仅信任 PATH 中绝对目录的入口软链，不放宽其他兼容目录。
	AllowSearchPathSymlinks bool
	// 由宿主排除不能作为命令使用的候选；被排除后继续查找。
	AcceptCandidate func(string) bool
}

type Resolver struct {
	goos                string
	directories         []string
	resolvedDirectories []string
	path                string
	pathext             []string
	userDirectories     []string
	acceptCandidate     func(string) bool
}

func NewResolver(options Options) (*Resolver, error) {
	goos := strings.ToLower(strings.TrimSpace(options.GOOS))
	if goos == "" {
		return nil, ErrInvalidOptions
	}
	directories := make([]string, 0, len(options.TrustedDirectories)+8)
	seen := make(map[string]struct{}, cap(directories))
	appendDirectory := func(value string, strict bool) error {
		value = strings.TrimSpace(value)
		if value == "." || !isAbsPathForGOOS(goos, value) {
			if strict {
				return ErrInvalidOptions
			}
			return nil
		}
		value = filepath.Clean(value)
		key := value
		if goos == "windows" {
			key = strings.ToLower(key)
		}
		if _, exists := seen[key]; exists {
			return nil
		}
		seen[key] = struct{}{}
		directories = append(directories, value)
		return nil
	}
	if options.SearchPathFirst {
		for _, directory := range splitSearchPath(options.SearchPath, goos) {
			if err := appendDirectory(directory, false); err != nil {
				return nil, err
			}
		}
	}
	for _, directory := range options.TrustedDirectories {
		if err := appendDirectory(directory, true); err != nil {
			return nil, err
		}
	}
	for _, directory := range splitSearchPath(options.SearchPath, goos) {
		if err := appendDirectory(directory, false); err != nil {
			return nil, err
		}
	}
	pathext := []string{".COM", ".EXE", ".BAT", ".CMD"}
	if goos == "windows" && strings.TrimSpace(options.PATHEXT) != "" {
		pathext = splitPATHEXT(options.PATHEXT)
	}
	targetDirectories := append([]string(nil), directories...)
	for _, directory := range options.TrustedTargetDirectories {
		directory = strings.TrimSpace(directory)
		if directory == "." || !isAbsPathForGOOS(goos, directory) {
			return nil, ErrInvalidOptions
		}
		targetDirectories = append(targetDirectories, filepath.Clean(directory))
	}
	var userDirectories []string
	if options.AllowSearchPathSymlinks {
		for _, directory := range splitSearchPath(options.SearchPath, goos) {
			directory = strings.TrimSpace(directory)
			if isAbsPathForGOOS(goos, directory) {
				userDirectories = append(userDirectories, filepath.Clean(directory))
			}
		}
	}
	return &Resolver{goos: goos, directories: directories, resolvedDirectories: resolvedDirectoryList(targetDirectories, goos), path: joinSearchPath(directories, goos), pathext: pathext,
		userDirectories: userDirectories, acceptCandidate: options.AcceptCandidate}, nil
}

func (resolver *Resolver) Resolve(name string) (string, error) {
	name = strings.TrimSpace(name)
	if resolver == nil || name == "" || name != filepath.Base(name) || strings.ContainsAny(name, "/\\\x00") {
		return "", ErrInvalidName
	}
	for _, directory := range resolver.directories {
		for _, candidateName := range resolver.candidateNames(name) {
			candidate := filepath.Join(directory, candidateName)
			info, err := os.Stat(candidate)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if resolver.goos != "windows" && runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
				continue
			}
			if !resolver.resolvedTargetTrusted(candidate) {
				continue
			}
			if resolver.acceptCandidate != nil && !resolver.acceptCandidate(candidate) {
				continue
			}
			return candidate, nil
		}
	}
	return "", ErrNotFound
}

func (resolver *Resolver) resolvedTargetTrusted(candidate string) bool {
	candidate = filepath.Clean(candidate)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return false
	}
	resolved = filepath.Clean(resolved)
	for _, directory := range resolver.userDirectories {
		if samePath(filepath.Dir(candidate), directory, resolver.goos) {
			return true
		}
	}
	if samePath(candidate, resolved, resolver.goos) {
		return true
	}
	return pathInsideAny(resolved, resolver.resolvedDirectories, resolver.goos)
}

func (resolver *Resolver) SearchPath() string {
	if resolver == nil {
		return ""
	}
	return resolver.path
}

func (resolver *Resolver) candidateNames(name string) []string {
	values := []string{name}
	if resolver.goos != "windows" || filepath.Ext(name) != "" {
		return values
	}
	for _, extension := range resolver.pathext {
		values = append(values, name+extension, name+strings.ToLower(extension))
	}
	return values
}

func splitSearchPath(value, goos string) []string {
	separator := ":"
	if goos == "windows" {
		separator = ";"
	}
	return strings.Split(value, separator)
}

func joinSearchPath(values []string, goos string) string {
	separator := ":"
	if goos == "windows" {
		separator = ";"
	}
	return strings.Join(values, separator)
}

func resolvedDirectoryList(directories []string, goos string) []string {
	values := make([]string, 0, len(directories))
	seen := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		resolved := filepath.Clean(directory)
		if value, err := filepath.EvalSymlinks(directory); err == nil {
			resolved = filepath.Clean(value)
		}
		key := normalizedPathKey(resolved, goos)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, resolved)
	}
	return values
}

func pathInsideAny(path string, roots []string, goos string) bool {
	for _, root := range roots {
		if pathInsideRoot(path, root, goos) {
			return true
		}
	}
	return false
}

func pathInsideRoot(path, root, goos string) bool {
	path = normalizedPathKey(filepath.Clean(path), goos)
	root = normalizedPathKey(filepath.Clean(root), goos)
	if path == root {
		return true
	}
	separator := string(filepath.Separator)
	return strings.HasPrefix(path, strings.TrimRight(root, separator)+separator)
}

func samePath(left, right, goos string) bool {
	return normalizedPathKey(filepath.Clean(left), goos) == normalizedPathKey(filepath.Clean(right), goos)
}

func normalizedPathKey(path, goos string) string {
	if goos == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func isAbsPathForGOOS(goos, value string) bool {
	if goos == "windows" {
		return filepath.IsAbs(value)
	}
	return filepath.IsAbs(value) || strings.HasPrefix(value, "/")
}

func splitPATHEXT(value string) []string {
	parts := strings.Split(value, ";")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToUpper(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, ".") {
			part = "." + part
		}
		result = append(result, part)
	}
	return result
}
