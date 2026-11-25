package runtimepaths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Layout 描述具体应用注入的运行目录命名，不携带任何产品默认值。
type Layout struct {
	ProductDirectory string
	ConfigFile       string
	DatabaseFile     string
	RuntimePrefix    string
	RootOverrideEnv  string
	EnvironmentEnv   string
	// InstanceDirectory 非空时,所有目录落到 <产品目录>/instances/<InstanceDirectory> 之下;
	// 为空表示默认实例,路径与历史版本完全一致。
	InstanceDirectory string
}

const instancesDirectoryName = "instances"

// Inputs 保存平台路径解析所需的可信系统目录，便于跨平台单元测试。
type Inputs struct {
	HomeDir      string
	UserCacheDir string
	LocalAppData string
	TempDir      string
	UID          string
}

// Paths 是产品运行时目录的唯一逻辑表示。
type Paths struct {
	Root          string
	ConfigDir     string
	ConfigFile    string
	ConfigBackups string
	DataDir       string
	DatabaseFile  string
	BlobsDir      string
	ArtifactsDir  string
	StateDir      string
	BackupsDir    string
	LogsDir       string
	CrashDir      string
	CacheDir      string
	RunDir        string
}

// Directories 返回启动时必须存在的全部私有目录。
func (paths Paths) Directories() []string {
	return []string{
		paths.Root,
		paths.ConfigDir,
		paths.ConfigBackups,
		paths.DataDir,
		paths.BlobsDir,
		paths.ArtifactsDir,
		paths.StateDir,
		paths.BackupsDir,
		paths.LogsDir,
		paths.CrashDir,
		paths.CacheDir,
		paths.RunDir,
	}
}

// Ensure 创建运行目录，并拒绝符号链接与 Windows reparse/junction。
func Ensure(paths Paths) error {
	for _, directory := range paths.Directories() {
		if err := EnsurePrivateDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

// EnsurePrivateDirectory 创建一个可由应用独占使用的私有目录。
func EnsurePrivateDirectory(directory string) error {
	if directory == "" {
		return errors.New("运行目录不能为空")
	}
	info, err := os.Lstat(directory)
	switch {
	case err == nil:
		if err := rejectOccupiedDirectory(directory, info); err != nil {
			return err
		}
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("创建运行目录 %s: %w", directory, err)
		}
	default:
		return fmt.Errorf("检查运行目录 %s: %w", directory, err)
	}
	info, err = os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("检查运行目录 %s: %w", directory, err)
	}
	if err := rejectOccupiedDirectory(directory, info); err != nil {
		return err
	}
	if err := lockdownPrivateDir(directory); err != nil {
		return fmt.Errorf("设置运行目录权限 %s: %w", directory, err)
	}
	return nil
}

func rejectOccupiedDirectory(directory string, info os.FileInfo) error {
	if isLinkLike(info) {
		return fmt.Errorf("运行目录不能是符号链接: %s", directory)
	}
	if !info.IsDir() {
		return fmt.Errorf("运行目录被普通文件占用: %s", directory)
	}
	return nil
}

// Resolve 只负责平台映射，不访问环境变量或文件系统。
func Resolve(goos string, layout Layout, input Inputs) (Paths, error) {
	if err := validateLayout(layout); err != nil {
		return Paths{}, err
	}
	switch goos {
	case "darwin":
		return resolveDarwin(layout, input)
	case "linux":
		return resolveLinux(layout, input)
	case "windows":
		return resolveWindows(layout, input)
	default:
		return Paths{}, fmt.Errorf("不支持的运行平台 %q", goos)
	}
}

// DevelopmentRoot 为开发和测试提供隔离目录，不用于正式发行配置。
func DevelopmentRoot(layout Layout, root string) (Paths, error) {
	if err := validateLayout(layout); err != nil {
		return Paths{}, err
	}
	if root == "" || !filepath.IsAbs(root) {
		return Paths{}, errors.New("开发运行根目录必须是绝对路径")
	}
	root = instanceRoot(layout, filepath.Join, root)
	return build(layout, filepath.Join, root, filepath.Join(root, "logs"), filepath.Join(root, "cache"), filepath.Join(root, "run")), nil
}

func developmentOverride(layout Layout) (Paths, bool, error) {
	if err := validateLayout(layout); err != nil {
		return Paths{}, true, err
	}
	root := os.Getenv(layout.RootOverrideEnv)
	if root == "" {
		return Paths{}, false, nil
	}
	environment := os.Getenv(layout.EnvironmentEnv)
	if environment != "development" && environment != "test" {
		return Paths{}, true, fmt.Errorf("%s 只允许在 development 或 test 环境使用", layout.RootOverrideEnv)
	}
	paths, err := DevelopmentRoot(layout, root)
	return paths, true, err
}

func resolveDarwin(layout Layout, input Inputs) (Paths, error) {
	if input.HomeDir == "" || input.UserCacheDir == "" || input.TempDir == "" || input.UID == "" {
		return Paths{}, errors.New("macOS 运行目录参数不完整")
	}

	root := instanceRoot(layout, joinDarwin, joinDarwin(input.HomeDir, "Library", "Application Support", layout.ProductDirectory))
	runPrefix := layout.RuntimePrefix + "-" + input.UID
	if layout.InstanceDirectory != "" {
		runPrefix += "-" + layout.InstanceDirectory
	}
	return build(layout, joinDarwin, root,
		instanceRoot(layout, joinDarwin, joinDarwin(input.HomeDir, "Library", "Logs", layout.ProductDirectory)),
		instanceRoot(layout, joinDarwin, joinDarwin(input.UserCacheDir, layout.ProductDirectory)),
		joinDarwin(input.TempDir, runPrefix)), nil
}

func resolveWindows(layout Layout, input Inputs) (Paths, error) {
	if input.LocalAppData == "" {
		return Paths{}, errors.New("Windows LOCALAPPDATA 不能为空")
	}

	root := instanceRoot(layout, joinWindows, joinWindows(input.LocalAppData, layout.ProductDirectory))
	return build(layout, joinWindows, root, joinWindows(root, "logs"), joinWindows(root, "cache"), joinWindows(root, "run")), nil
}

// instanceRoot 把默认实例根映射到实例子目录;默认实例保持原路径不变。
func instanceRoot(layout Layout, join func(...string) string, root string) string {
	if layout.InstanceDirectory == "" {
		return root
	}
	return join(root, instancesDirectoryName, layout.InstanceDirectory)
}

func joinDarwin(parts ...string) string {
	return joinWith("/", parts...)
}

func joinWindows(parts ...string) string {
	return joinWith(`\`, parts...)
}

func joinWith(separator string, parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for index, part := range parts {
		if part == "" {
			continue
		}
		if index == 0 {
			cleaned = append(cleaned, strings.TrimRight(part, `\/`))
			continue
		}
		cleaned = append(cleaned, strings.Trim(part, `\/`))
	}
	return strings.Join(cleaned, separator)
}

func build(layout Layout, join func(...string) string, root, logs, cache, run string) Paths {
	config := join(root, "config")
	data := join(root, "data")
	return Paths{
		Root:          root,
		ConfigDir:     config,
		ConfigFile:    join(config, layout.ConfigFile),
		ConfigBackups: join(config, "backups"),
		DataDir:       data,
		DatabaseFile:  join(data, layout.DatabaseFile),
		BlobsDir:      join(data, "blobs", "sha256"),
		ArtifactsDir:  join(data, "artifacts"),
		StateDir:      join(root, "state"),
		BackupsDir:    join(root, "backups"),
		LogsDir:       logs,
		CrashDir:      join(logs, "crash"),
		CacheDir:      cache,
		RunDir:        run,
	}
}

func validateLayout(layout Layout) error {
	values := map[string]string{
		"产品目录名":  layout.ProductDirectory,
		"配置文件名":  layout.ConfigFile,
		"数据库文件名": layout.DatabaseFile,
		"运行目录前缀": layout.RuntimePrefix,
	}
	for label, value := range values {
		if !validPortableSegment(value) {
			return fmt.Errorf("%s必须是安全的单段名称", label)
		}
	}
	if !validEnvironmentName(layout.RootOverrideEnv) || !validEnvironmentName(layout.EnvironmentEnv) {
		return errors.New("运行目录环境变量名无效")
	}
	if layout.InstanceDirectory != "" && !validInstanceSegment(layout.InstanceDirectory) {
		return errors.New("实例目录名必须是 8 到 32 位小写十六进制")
	}
	return nil
}

func validInstanceSegment(value string) bool {
	if len(value) < 8 || len(value) > 32 {
		return false
	}
	for _, character := range value {
		if character >= '0' && character <= '9' || character >= 'a' && character <= 'f' {
			continue
		}
		return false
	}
	return true
}

func validEnvironmentName(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if character == '_' || character >= 'A' && character <= 'Z' || index > 0 && character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}

func validPortableSegment(value string) bool {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `<>:"/\\|?*`) {
		return false
	}
	if strings.HasSuffix(value, ".") || strings.HasSuffix(value, " ") {
		return false
	}
	for _, character := range value {
		if character < 32 {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(value, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	default:
		return true
	}
}

// resolveLinux 遵循用户数据与缓存分离，实例目录由公共布局统一隔离。
func resolveLinux(layout Layout, input Inputs) (Paths, error) {
	if input.HomeDir == "" || input.UserCacheDir == "" || input.TempDir == "" || input.UID == "" {
		return Paths{}, errors.New("Linux 运行目录参数不完整")
	}
	root := instanceRoot(layout, joinDarwin, joinDarwin(input.HomeDir, ".local", "share", layout.ProductDirectory))
	cache := instanceRoot(layout, joinDarwin, joinDarwin(input.UserCacheDir, layout.ProductDirectory))
	runPrefix := layout.RuntimePrefix + "-" + input.UID
	if layout.InstanceDirectory != "" {
		runPrefix += "-" + layout.InstanceDirectory
	}
	return build(layout, joinDarwin, root, joinDarwin(cache, "logs"), cache, joinDarwin(input.TempDir, runPrefix)), nil
}
