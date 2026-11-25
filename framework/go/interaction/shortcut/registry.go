package shortcut

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

type Platform string

const (
	PlatformMacOS   Platform = "macos"
	PlatformWindows Platform = "windows"
)

type Modifier string

const (
	ModCtrl      Modifier = "ctrl"
	ModAlt       Modifier = "alt"
	ModShift     Modifier = "shift"
	ModMeta      Modifier = "meta"
	ModCmdOrCtrl Modifier = "cmd-or-ctrl"
)

var (
	ErrInvalidBinding = errors.New("快捷键绑定无效")
	ErrConflict       = errors.New("快捷键绑定冲突")
	ErrNotFound       = errors.New("快捷键绑定不存在")
)

type Accelerator struct {
	Key       string
	Modifiers []Modifier
}

type Binding struct {
	ID          string
	CommandType string
	Scope       string
	Platforms   []Platform
	Accelerator Accelerator
}

type Registry struct {
	mu       sync.RWMutex
	byID     map[string]Binding
	byLookup map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		byID:     make(map[string]Binding),
		byLookup: make(map[string]string),
	}
}

func (registry *Registry) Register(binding Binding) error {
	normalized, err := normalizeBinding(binding)
	if err != nil {
		return err
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.byID[normalized.ID]; exists {
		return ErrConflict
	}
	keys := lookupKeys(normalized)
	for _, key := range keys {
		if existingID, exists := registry.byLookup[key]; exists && existingID != normalized.ID {
			return ErrConflict
		}
	}
	registry.byID[normalized.ID] = normalized
	for _, key := range keys {
		registry.byLookup[key] = normalized.ID
	}
	return nil
}

func (registry *Registry) Resolve(platform Platform, scope string, accelerator Accelerator) (Binding, error) {
	if !platform.Valid() || strings.TrimSpace(scope) == "" {
		return Binding{}, ErrInvalidBinding
	}
	normalized, err := accelerator.Normalized()
	if err != nil {
		return Binding{}, err
	}
	key := makeLookupKey(platform, normalizeScope(scope), normalized)
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	id, exists := registry.byLookup[key]
	if !exists {
		return Binding{}, ErrNotFound
	}
	return registry.byID[id], nil
}

func (registry *Registry) Get(id string) (Binding, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	binding, exists := registry.byID[id]
	if !exists {
		return Binding{}, ErrNotFound
	}
	return binding, nil
}

func (accelerator Accelerator) Speak(platform Platform) string {
	names := map[Modifier]string{
		ModCtrl:      "Control",
		ModAlt:       "Option",
		ModShift:     "Shift",
		ModMeta:      "命令",
		ModCmdOrCtrl: "命令",
	}
	if platform == PlatformWindows {
		names[ModAlt] = "Alt"
		names[ModMeta] = "Win"
		names[ModCmdOrCtrl] = "Control"
	}
	parts := make([]string, 0, len(accelerator.Modifiers)+1)
	for _, modifier := range accelerator.Modifiers {
		if name := names[modifier]; name != "" {
			parts = append(parts, name)
		}
	}
	if key := spokenAcceleratorKey(accelerator.Key); key != "" {
		parts = append(parts, key)
	}
	if len(parts) == 0 {
		return "空"
	}
	return strings.Join(parts, "+")
}

func spokenAcceleratorKey(key string) string {
	if strings.TrimSpace(key) == "" && key != "" {
		return "空格"
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "arrowdown":
		return "下箭头"
	case "arrowup":
		return "上箭头"
	case "arrowleft":
		return "左箭头"
	case "arrowright":
		return "右箭头"
	case "home":
		return "行首"
	case "end":
		return "行尾"
	case "escape":
		return "退出"
	case "tab":
		return "制表符"
	case "enter", "return":
		return "回车"
	case " ", "space", "spacebar":
		return "空格"
	case "delete", "backspace":
		return "删除"
	case "pageup":
		return "上一页"
	case "pagedown":
		return "下一页"
	case "insert":
		return "插入"
	case "+", "add":
		return "加号"
	case "-", "subtract":
		return "减号"
	case "*":
		return "星号"
	case "=", "equal":
		return "等号"
	case "/", "slash":
		return "斜杠"
	case ",", "comma":
		return "逗号"
	case ".", "period":
		return "句号"
	case "[", "bracketleft":
		return "左方括号"
	case "]", "bracketright":
		return "右方括号"
	case ";", "semicolon":
		return "分号"
	case "'", "quote":
		return "引号"
	case "\\", "backslash":
		return "反斜杠"
	case "`", "backquote":
		return "重音符"
	case "{", "braceleft":
		return "左花括号"
	case "}", "braceright":
		return "右花括号"
	case "(":
		return "左圆括号"
	case ")":
		return "右圆括号"
	case "|", "bar":
		return "竖线"
	case "?":
		return "问号"
	case "!":
		return "感叹号"
	case "@":
		return "商业at"
	case "#":
		return "井号"
	case "$":
		return "美元符号"
	case "%":
		return "百分号"
	case "^":
		return "插入符号"
	case "&":
		return "和号"
	case "<":
		return "小于号"
	case ">":
		return "大于号"
	case "capslock":
		return "大写锁定"
	case "contextmenu":
		return "菜单"
	case "printscreen":
		return "打印屏幕"
	case "scrolllock":
		return "滚动锁定"
	case "pause":
		return "暂停"
	case "numlock":
		return "数字锁定"
	case "":
		return ""
	default:
		return strings.ToUpper(strings.TrimSpace(key))
	}
}

func (accelerator Accelerator) Normalized() (Accelerator, error) {
	key := strings.ToLower(strings.TrimSpace(accelerator.Key))
	if key == "" {
		return Accelerator{}, ErrInvalidBinding
	}
	seen := make(map[Modifier]struct{})
	modifiers := make([]Modifier, 0, len(accelerator.Modifiers))
	for _, modifier := range accelerator.Modifiers {
		normalized := Modifier(strings.ToLower(strings.TrimSpace(string(modifier))))
		if !normalized.Valid() {
			return Accelerator{}, ErrInvalidBinding
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		modifiers = append(modifiers, normalized)
	}
	sort.Slice(modifiers, func(i, j int) bool {
		return modifierRank(modifiers[i]) < modifierRank(modifiers[j])
	})
	return Accelerator{Key: key, Modifiers: modifiers}, nil
}

func (platform Platform) Valid() bool {
	switch platform {
	case PlatformMacOS, PlatformWindows:
		return true
	default:
		return false
	}
}

func (modifier Modifier) Valid() bool {
	switch modifier {
	case ModCtrl, ModAlt, ModShift, ModMeta, ModCmdOrCtrl:
		return true
	default:
		return false
	}
}

func normalizeBinding(binding Binding) (Binding, error) {
	if strings.TrimSpace(binding.ID) == "" || strings.TrimSpace(binding.CommandType) == "" || strings.TrimSpace(binding.Scope) == "" {
		return Binding{}, ErrInvalidBinding
	}
	accelerator, err := binding.Accelerator.Normalized()
	if err != nil {
		return Binding{}, err
	}
	platforms := binding.Platforms
	if len(platforms) == 0 {
		platforms = []Platform{PlatformMacOS, PlatformWindows}
	}
	seen := make(map[Platform]struct{})
	normalizedPlatforms := make([]Platform, 0, len(platforms))
	for _, platform := range platforms {
		if !platform.Valid() {
			return Binding{}, ErrInvalidBinding
		}
		if _, exists := seen[platform]; exists {
			continue
		}
		seen[platform] = struct{}{}
		normalizedPlatforms = append(normalizedPlatforms, platform)
	}
	sort.Slice(normalizedPlatforms, func(i, j int) bool {
		return normalizedPlatforms[i] < normalizedPlatforms[j]
	})
	return Binding{
		ID:          strings.TrimSpace(binding.ID),
		CommandType: strings.TrimSpace(binding.CommandType),
		Scope:       normalizeScope(binding.Scope),
		Platforms:   normalizedPlatforms,
		Accelerator: accelerator,
	}, nil
}

func lookupKeys(binding Binding) []string {
	keys := make([]string, 0, len(binding.Platforms))
	for _, platform := range binding.Platforms {
		keys = append(keys, makeLookupKey(platform, binding.Scope, binding.Accelerator))
	}
	return keys
}

func makeLookupKey(platform Platform, scope string, accelerator Accelerator) string {
	parts := make([]string, 0, len(accelerator.Modifiers)+3)
	parts = append(parts, string(platform), scope)
	for _, modifier := range accelerator.Modifiers {
		parts = append(parts, string(modifier))
	}
	parts = append(parts, accelerator.Key)
	return strings.Join(parts, "|")
}

func normalizeScope(scope string) string {
	return strings.ToLower(strings.TrimSpace(scope))
}

func modifierRank(modifier Modifier) int {
	switch modifier {
	case ModCmdOrCtrl:
		return 10
	case ModCtrl:
		return 20
	case ModAlt:
		return 30
	case ModShift:
		return 40
	case ModMeta:
		return 50
	default:
		return 100
	}
}
