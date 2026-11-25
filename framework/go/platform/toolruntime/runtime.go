package toolruntime

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const ManifestName = ".mz-gui-runtime.json"

var componentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type Spec struct {
	HostExecutable string
	RuntimeID      string
	Version        string
	Arch           string
	ManifestSHA256 string
}

type Resolved struct {
	Directory  string
	Executable string
	Manifest   Manifest
}

type Manifest struct {
	SchemaVersion  int          `json:"schema_version"`
	RuntimeID      string       `json:"runtime_id"`
	Version        string       `json:"version"`
	Arch           string       `json:"arch"`
	Entrypoint     string       `json:"entrypoint"`
	EntrypointArch string       `json:"entrypoint_arch"`
	ProtectedRoots []string     `json:"protected_roots"`
	Files          []FileRecord `json:"files"`
}

type FileRecord struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Resolve 只接受与宿主程序同级、描述摘要已嵌入宿主程序的工具运行时。
func Resolve(spec Spec) (Resolved, error) {
	host := filepath.Clean(spec.HostExecutable)
	if !filepath.IsAbs(host) || isUNC(host) {
		return Resolved{}, errors.New("宿主程序路径必须是本地绝对路径")
	}
	if err := validateRegularFile(host); err != nil {
		return Resolved{}, fmt.Errorf("校验宿主程序: %w", err)
	}
	if !validComponent(spec.RuntimeID) || !validComponent(spec.Version) {
		return Resolved{}, errors.New("工具运行时标识或版本无效")
	}
	if _, err := machineForArch(spec.Arch); err != nil {
		return Resolved{}, err
	}
	if !validDigest(spec.ManifestSHA256) {
		return Resolved{}, errors.New("工具运行时描述摘要无效")
	}
	runtimesRoot := filepath.Join(filepath.Dir(host), "tool-runtimes")
	runtimeDirectory := filepath.Join(runtimesRoot, spec.RuntimeID, spec.Version)
	for _, directory := range []string{runtimesRoot, filepath.Join(runtimesRoot, spec.RuntimeID), runtimeDirectory} {
		if err := validateDirectory(directory); err != nil {
			return Resolved{}, fmt.Errorf("校验工具运行时目录: %w", err)
		}
	}
	if err := ensureContained(runtimesRoot, runtimeDirectory); err != nil {
		return Resolved{}, err
	}
	return ValidateDirectory(runtimeDirectory, spec.RuntimeID, spec.Version, spec.Arch, spec.ManifestSHA256)
}

// ValidateDirectory 校验已经定位的工具运行时，供安装器准备工具和宿主程序共用。
func ValidateDirectory(runtimeDirectory, runtimeID, version, arch, manifestSHA256 string) (Resolved, error) {
	if !filepath.IsAbs(runtimeDirectory) || isUNC(runtimeDirectory) {
		return Resolved{}, errors.New("工具运行时目录必须是本地绝对路径")
	}
	if !validComponent(runtimeID) || !validComponent(version) || !validDigest(manifestSHA256) {
		return Resolved{}, errors.New("工具运行时校验约束无效")
	}
	if _, err := machineForArch(arch); err != nil {
		return Resolved{}, err
	}
	if err := validateDirectory(runtimeDirectory); err != nil {
		return Resolved{}, fmt.Errorf("校验工具运行时目录: %w", err)
	}
	manifest, err := loadManifest(runtimeDirectory, manifestSHA256)
	if err != nil {
		return Resolved{}, err
	}
	if manifest.RuntimeID != runtimeID || manifest.Version != version || manifest.Arch != arch {
		return Resolved{}, errors.New("工具运行时描述与宿主约束不匹配")
	}
	if err := validateManifest(runtimeDirectory, manifest); err != nil {
		return Resolved{}, err
	}
	return Resolved{Directory: runtimeDirectory, Executable: filepath.Join(runtimeDirectory, filepath.FromSlash(manifest.Entrypoint)), Manifest: manifest}, nil
}

func loadManifest(runtimeDirectory, expectedDigest string) (Manifest, error) {
	path := filepath.Join(runtimeDirectory, ManifestName)
	if err := validateRegularFile(path); err != nil {
		return Manifest{}, fmt.Errorf("校验工具运行时描述文件: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return Manifest{}, errors.New("工具运行时描述摘要不匹配")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("解析工具运行时描述: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("工具运行时描述包含多余 JSON 值")
	}
	return manifest, nil
}

func validateManifest(root string, manifest Manifest) error {
	if manifest.SchemaVersion != 1 || !validComponent(manifest.RuntimeID) || !validComponent(manifest.Version) || len(manifest.Files) == 0 || len(manifest.ProtectedRoots) == 0 {
		return errors.New("工具运行时描述结构无效")
	}
	entrypoint, ok := cleanRelative(manifest.Entrypoint)
	if !ok {
		return errors.New("工具运行时入口无效")
	}
	machine, err := machineForArch(manifest.EntrypointArch)
	if err != nil {
		return errors.New("工具运行时入口架构无效")
	}
	protected := make(map[string]struct{}, len(manifest.ProtectedRoots))
	for _, value := range manifest.ProtectedRoots {
		cleaned, valid := cleanRelative(value)
		if !valid || cleaned != value {
			return errors.New("工具运行时保护目录无效")
		}
		protected[value] = struct{}{}
	}
	seen := make(map[string]FileRecord, len(manifest.Files))
	previous := ""
	for _, record := range manifest.Files {
		cleaned, valid := cleanRelative(record.Path)
		if !valid || cleaned != record.Path || record.Size <= 0 || !validDigest(record.SHA256) || record.Path <= previous {
			return errors.New("工具运行时文件记录无效或未排序")
		}
		previous = record.Path
		path := filepath.Join(root, filepath.FromSlash(record.Path))
		if err := ensureContained(root, path); err != nil {
			return err
		}
		if err := verifyFile(path, record); err != nil {
			return fmt.Errorf("校验工具运行时文件 %s: %w", record.Path, err)
		}
		seen[record.Path] = record
	}
	if _, exists := seen[entrypoint]; !exists {
		return errors.New("工具运行时入口未纳入完整性清单")
	}
	for protectedRoot := range protected {
		directory := filepath.Join(root, filepath.FromSlash(protectedRoot))
		if err := validateDirectory(directory); err != nil {
			return fmt.Errorf("校验工具运行时保护目录 %s: %w", protectedRoot, err)
		}
		if err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == directory {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
				return errors.New("保护目录包含符号链接或特殊文件")
			}
			if info.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if _, exists := seen[filepath.ToSlash(relative)]; !exists {
				return fmt.Errorf("保护目录包含未登记文件 %s", filepath.ToSlash(relative))
			}
			return nil
		}); err != nil {
			return err
		}
	}
	actualMachine, err := readPEMachine(filepath.Join(root, filepath.FromSlash(entrypoint)))
	if err != nil {
		return fmt.Errorf("校验工具运行时入口 PE: %w", err)
	}
	if actualMachine != machine {
		return fmt.Errorf("工具运行时架构不匹配: machine=0x%04x", actualMachine)
	}
	return nil
}

func verifyFile(path string, record FileRecord) error {
	if err := validateRegularFile(path); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != record.Size {
		return errors.New("文件大小不匹配")
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	if hex.EncodeToString(hasher.Sum(nil)) != record.SHA256 {
		return errors.New("文件摘要不匹配")
	}
	return nil
}

func cleanRelative(value string) (string, bool) {
	if value == "" || strings.Contains(value, `\\`) || filepath.IsAbs(value) {
		return "", false
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func validComponent(value string) bool { return componentPattern.MatchString(value) }

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

func machineForArch(arch string) (uint16, error) {
	switch arch {
	case "amd64":
		return 0x8664, nil
	case "arm64":
		return 0xAA64, nil
	default:
		return 0, fmt.Errorf("不支持的工具运行时架构 %q", arch)
	}
}

func validateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("必须是目录且不能是符号链接")
	}
	return nil
}

func validateRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 {
		return errors.New("必须是非空普通文件且不能是符号链接")
	}
	return nil
}

func ensureContained(root, target string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(realRoot, realTarget)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("工具运行时路径越出固定根目录")
	}
	return nil
}

func readPEMachine(path string) (uint16, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	header := make([]byte, 64)
	if _, err := io.ReadFull(file, header); err != nil || header[0] != 'M' || header[1] != 'Z' {
		return 0, errors.New("DOS 头无效")
	}
	offset := int64(binary.LittleEndian.Uint32(header[0x3c:]))
	info, err := file.Stat()
	if err != nil || offset < 64 || offset > info.Size()-6 || offset > 16<<20 {
		return 0, errors.New("PE 头偏移无效")
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	pe := make([]byte, 6)
	if _, err := io.ReadFull(file, pe); err != nil || string(pe[:4]) != "PE\x00\x00" {
		return 0, errors.New("PE 签名无效")
	}
	return binary.LittleEndian.Uint16(pe[4:]), nil
}

func isUNC(path string) bool { return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `//`) }

// SortFileRecords 供发布工具生成确定性的描述文件。
func SortFileRecords(records []FileRecord) {
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
}
