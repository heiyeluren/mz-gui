package executable

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	LauncherNative      = "native"
	LauncherNodeWrapper = "node-wrapper"
	LauncherShellShim   = "shell-shim"
)

var (
	ErrUnsupportedLauncher = errors.New("不支持的可执行文件启动形态")
	ErrNodeRequired        = errors.New("node-wrapper 缺少 node 可执行文件")
)

type Launch struct {
	Path         string
	ResolvedPath string
	Kind         string
	RequiresNode bool
	NodePath     string
}

// Inspect 按 0.4.35 launcherKind 识别 native / node-wrapper / shell-shim。
func Inspect(path string) (Launch, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || !filepath.IsAbs(path) {
		return Launch{}, ErrInvalidName
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return Launch{}, ErrNotFound
	}
	header, err := readHeader(resolved)
	if err != nil {
		return Launch{}, ErrNotFound
	}
	kind, declaredNode := classifyHeader(header, resolved)
	if kind == "" {
		return Launch{}, ErrUnsupportedLauncher
	}
	launch := Launch{
		Path: path, ResolvedPath: resolved, Kind: kind,
		RequiresNode: kind == LauncherNodeWrapper, NodePath: declaredNode,
	}
	return launch, nil
}

func ExecutableFingerprint(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || !filepath.IsAbs(path) {
		return "", ErrInvalidName
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	return fileFingerprint(path, resolved)
}

func LaunchFingerprint(launch Launch) (string, error) {
	hasher := sha256.New()
	pathFingerprint, err := fileFingerprint(launch.Path, launch.ResolvedPath)
	if err != nil {
		return "", err
	}
	_, _ = fmt.Fprintf(hasher, "path=%s\nresolved=%s\nkind=%s\nrequires_node=%t\nfingerprint=%s\n",
		filepath.Clean(launch.Path), filepath.Clean(launch.ResolvedPath), launch.Kind, launch.RequiresNode, pathFingerprint)
	if launch.Kind == LauncherNodeWrapper {
		nodePath := filepath.Clean(strings.TrimSpace(launch.NodePath))
		if nodePath == "." || !filepath.IsAbs(nodePath) {
			return "", ErrNodeRequired
		}
		nodeFingerprint, err := ExecutableFingerprint(nodePath)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hasher, "node=%s\nnode_fingerprint=%s\n", nodePath, nodeFingerprint)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (resolver *Resolver) ResolveLaunch(name string) (Launch, error) {
	path, err := resolver.Resolve(name)
	if err != nil {
		return Launch{}, err
	}
	launch, err := Inspect(path)
	if err != nil {
		return Launch{}, err
	}
	if launch.Kind == LauncherNodeWrapper && launch.NodePath == "" {
		node, nodeErr := resolver.Resolve("node")
		if nodeErr != nil {
			return Launch{}, ErrNodeRequired
		}
		launch.NodePath = node
	}
	return launch, nil
}

func Apply(launch Launch, arguments []string) (string, []string, error) {
	switch launch.Kind {
	case LauncherNodeWrapper:
		if strings.TrimSpace(launch.NodePath) == "" {
			return "", nil, ErrNodeRequired
		}
		return launch.NodePath, append([]string{launch.Path}, arguments...), nil
	case LauncherNative, LauncherShellShim:
		if strings.TrimSpace(launch.Path) == "" {
			return "", nil, ErrUnsupportedLauncher
		}
		return launch.Path, arguments, nil
	default:
		return "", nil, ErrUnsupportedLauncher
	}
}

func fileFingerprint(path, resolved string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	resolved = filepath.Clean(strings.TrimSpace(resolved))
	if path == "." || !filepath.IsAbs(path) {
		return "", ErrInvalidName
	}
	if resolved == "." || !filepath.IsAbs(resolved) {
		resolved = path
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrNotFound
	}
	file, err := os.Open(resolved)
	if err != nil {
		return "", err
	}
	defer file.Close()
	headHasher := sha256.New()
	if _, err := io.CopyN(headHasher, file, 8192); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	hasher := sha256.New()
	_, _ = fmt.Fprintf(hasher, "path=%s\nresolved=%s\nsize=%d\nmode=%s\nmtime=%d\nhead=%s\n",
		path, resolved, info.Size(), info.Mode().String(), info.ModTime().UnixNano(), hex.EncodeToString(headHasher.Sum(nil)))
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func readHeader(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	header := make([]byte, 8192)
	n, err := file.Read(header)
	if n == 0 {
		return nil, err
	}
	return header[:n], nil
}

func classifyHeader(header []byte, resolved string) (string, string) {
	if isNativeMagic(header) {
		return LauncherNative, ""
	}
	firstLine, _, _ := bytes.Cut(header, []byte{'\n'})
	line := strings.TrimSpace(string(bytes.TrimSuffix(firstLine, []byte{'\r'})))
	if strings.HasPrefix(line, "#!") {
		parts := strings.Fields(strings.TrimSpace(line[2:]))
		if len(parts) > 0 {
			commandParts := parts
			interpreter := parts[0]
			if strings.EqualFold(filepath.Base(interpreter), "env") {
				commandParts = parts[1:]
				for len(commandParts) > 0 && strings.HasPrefix(commandParts[0], "-") {
					commandParts = commandParts[1:]
				}
				interpreter = ""
				if len(commandParts) > 0 {
					interpreter = commandParts[0]
				}
			}
			switch strings.ToLower(filepath.Base(interpreter)) {
			case "node", "nodejs":
				if len(commandParts) == 1 {
					declared := ""
					if filepath.IsAbs(interpreter) {
						declared = interpreter
					}
					return LauncherNodeWrapper, declared
				}
			case "sh", "bash", "zsh", "dash":
				return LauncherShellShim, ""
			}
		}
	}
	switch strings.ToLower(filepath.Ext(resolved)) {
	case ".js", ".mjs", ".cjs":
		return LauncherNodeWrapper, ""
	case ".cmd", ".bat", ".com":
		return LauncherShellShim, ""
	}
	return "", ""
}

func isNativeMagic(header []byte) bool {
	magics := [][]byte{
		{0xcf, 0xfa, 0xed, 0xfe},
		{0xce, 0xfa, 0xed, 0xfe},
		{0xfe, 0xed, 0xfa, 0xcf},
		{0xfe, 0xed, 0xfa, 0xce},
		{0xca, 0xfe, 0xba, 0xbe},
		{0xbe, 0xba, 0xfe, 0xca},
		{0xca, 0xfe, 0xba, 0xbf},
		{0xbf, 0xba, 0xfe, 0xca},
		{0x7f, 'E', 'L', 'F'},
		{'M', 'Z'},
	}
	for _, magic := range magics {
		if bytes.HasPrefix(header, magic) {
			return true
		}
	}
	return false
}
