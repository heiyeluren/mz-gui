package appmanifest

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/heiyeluren/mz-gui/framework/go/core/capability"
	"github.com/pelletier/go-toml/v2"
)

const SchemaVersion = 1

var (
	ErrInvalidManifest = errors.New("应用 Manifest 无效")

	reverseDomainPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)+$`)
	protocolPattern      = regexp.MustCompile(`^[a-z][a-z0-9+.-]{1,31}$`)
	namespacePattern     = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)
	processPattern       = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
	windowsAppIDPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.-]{2,127}$`)
	adapterPattern       = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
)

type App struct {
	ID               string `toml:"id"`
	DisplayName      string `toml:"display_name"`
	Description      string `toml:"description"`
	ProtocolScheme   string `toml:"protocol_scheme"`
	DataNamespace    string `toml:"data_namespace"`
	SingleInstanceID string `toml:"single_instance_id"`
}

type Processes struct {
	Desktop     string `toml:"desktop"`
	Agentd      string `toml:"agentd"`
	BrowserHost string `toml:"browser_host"`
	Helper      string `toml:"helper"`
}

type Storage struct {
	ConfigBasename    string `toml:"config_basename"`
	DatabaseBasename  string `toml:"database_basename"`
	ArtifactNamespace string `toml:"artifact_namespace"`
}

type Framework struct {
	CapabilityCatalogVersion int      `toml:"capability_catalog_version"`
	Capabilities             []string `toml:"capabilities"`
	Adapters                 []string `toml:"adapters"`
}

type Security struct {
	CredentialService string `toml:"credential_service"`
}

type Build struct {
	MacOSBundleID string   `toml:"macos_bundle_id"`
	WindowsAppID  string   `toml:"windows_app_id"`
	Channels      []string `toml:"channels"`
}

type Manifest struct {
	SchemaVersion int       `toml:"schema_version"`
	App           App       `toml:"app"`
	Processes     Processes `toml:"processes"`
	Storage       Storage   `toml:"storage"`
	Framework     Framework `toml:"framework"`
	Security      Security  `toml:"security"`
	Build         Build     `toml:"build"`
}

type Validated struct {
	Manifest  Manifest
	Selection capability.Selection
}

func Parse(data []byte, catalog capability.Catalog) (Validated, error) {
	var manifest Manifest
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Validated{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if err := validateManifest(manifest, catalog); err != nil {
		return Validated{}, err
	}
	requested := append([]string(nil), manifest.Framework.Capabilities...)
	for _, adapter := range manifest.Framework.Adapters {
		requested = append(requested, "adapter."+adapter)
	}
	selection, err := catalog.Resolve(requested)
	if err != nil {
		return Validated{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if !sameStringSet(requested, selection.Resolved) {
		return Validated{}, fmt.Errorf("%w: capability 依赖必须显式声明", ErrInvalidManifest)
	}
	return Validated{Manifest: manifest, Selection: selection}, nil
}

func (validated Validated) ValidateProduction(catalog capability.Catalog) error {
	return catalog.ValidateProduction(validated.Selection)
}

func validateManifest(manifest Manifest, catalog capability.Catalog) error {
	if manifest.SchemaVersion != SchemaVersion || manifest.Framework.CapabilityCatalogVersion != catalog.SchemaVersion {
		return fmt.Errorf("%w: schema 或 Catalog 主版本不兼容", ErrInvalidManifest)
	}
	if !reverseDomainPattern.MatchString(manifest.App.ID) || !validDisplayText(manifest.App.DisplayName, 80) ||
		!validDisplayText(manifest.App.Description, 240) || !protocolPattern.MatchString(manifest.App.ProtocolScheme) ||
		!namespacePattern.MatchString(manifest.App.DataNamespace) || !reverseDomainPattern.MatchString(manifest.App.SingleInstanceID) {
		return fmt.Errorf("%w: 应用身份字段无效", ErrInvalidManifest)
	}
	if err := validateProcesses(manifest.Processes); err != nil {
		return err
	}
	if !validBasename(manifest.Storage.ConfigBasename, ".toml") ||
		!validBasename(manifest.Storage.DatabaseBasename, ".db") ||
		!namespacePattern.MatchString(manifest.Storage.ArtifactNamespace) {
		return fmt.Errorf("%w: 存储 basename 或 namespace 无效", ErrInvalidManifest)
	}
	if !reverseDomainPattern.MatchString(manifest.Security.CredentialService) ||
		!reverseDomainPattern.MatchString(manifest.Build.MacOSBundleID) || manifest.Build.MacOSBundleID != manifest.App.ID ||
		!windowsAppIDPattern.MatchString(manifest.Build.WindowsAppID) {
		return fmt.Errorf("%w: 发布或凭据身份无效", ErrInvalidManifest)
	}
	if len(manifest.Framework.Capabilities) == 0 || hasDuplicates(manifest.Framework.Capabilities) || hasDuplicates(manifest.Framework.Adapters) {
		return fmt.Errorf("%w: capability 或 adapter 列表无效", ErrInvalidManifest)
	}
	for _, adapter := range manifest.Framework.Adapters {
		if !adapterPattern.MatchString(adapter) {
			return fmt.Errorf("%w: adapter 名称无效", ErrInvalidManifest)
		}
	}
	if err := validateChannels(manifest.Build.Channels); err != nil {
		return err
	}
	return nil
}

func validateProcesses(processes Processes) error {
	values := []string{processes.Desktop, processes.Agentd, processes.BrowserHost, processes.Helper}
	if processes.Desktop == "" {
		return fmt.Errorf("%w: desktop 进程名不能为空", ErrInvalidManifest)
	}
	seen := make(map[string]struct{}, len(values))
	reserved := map[string]bool{"con": true, "prn": true, "aux": true, "nul": true, "clock$": true,
		"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
		"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true}
	for _, value := range values {
		if value == "" {
			continue
		}
		if !processPattern.MatchString(value) || reserved[strings.ToLower(value)] {
			return fmt.Errorf("%w: 进程名 %q 无效", ErrInvalidManifest, value)
		}
		if _, exists := seen[strings.ToLower(value)]; exists {
			return fmt.Errorf("%w: 进程名不能重复", ErrInvalidManifest)
		}
		seen[strings.ToLower(value)] = struct{}{}
	}
	return nil
}

func validateChannels(channels []string) error {
	if len(channels) == 0 || hasDuplicates(channels) {
		return fmt.Errorf("%w: 发布渠道无效", ErrInvalidManifest)
	}
	allowed := map[string]bool{"dev": true, "beta": true, "stable": true}
	for _, channel := range channels {
		if !allowed[channel] {
			return fmt.Errorf("%w: 发布渠道 %q 无效", ErrInvalidManifest, channel)
		}
	}
	return nil
}

func validBasename(value, suffix string) bool {
	if len(value) < len(suffix)+1 || len(value) > 80 || !strings.HasSuffix(value, suffix) || strings.ContainsAny(value, `/\\`) {
		return false
	}
	stem := strings.TrimSuffix(value, suffix)
	return namespacePattern.MatchString(stem)
}

func validDisplayText(value string, maximum int) bool {
	if value == "" || len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func hasDuplicates(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	set := make(map[string]struct{}, len(left))
	for _, value := range left {
		set[value] = struct{}{}
	}
	for _, value := range right {
		if _, exists := set[value]; !exists {
			return false
		}
	}
	return true
}
