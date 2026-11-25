package shortcut

import (
	"errors"
	"testing"
)

func TestRegistryRegistersAndResolvesShortcut(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(Binding{
		ID:          "chat.send",
		CommandType: "chat.send",
		Scope:       "Chat",
		Accelerator: Accelerator{Key: "Enter", Modifiers: []Modifier{ModCmdOrCtrl, ModShift, ModShift}},
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := registry.Resolve(PlatformMacOS, "chat", Accelerator{Key: "enter", Modifiers: []Modifier{ModShift, ModCmdOrCtrl}})
	if err != nil {
		t.Fatal(err)
	}
	if binding.ID != "chat.send" || binding.Accelerator.Key != "enter" || len(binding.Accelerator.Modifiers) != 2 {
		t.Fatalf("解析结果错误: %#v", binding)
	}
	if got := binding.Accelerator.Speak(PlatformMacOS); got != "命令+Shift+回车" {
		t.Fatalf("macOS 读法 = %s", got)
	}
}

func TestRegistryRejectsConflictInSamePlatformAndScope(t *testing.T) {
	registry := NewRegistry()
	first := Binding{
		ID:          "chat.send",
		CommandType: "chat.send",
		Scope:       "chat",
		Platforms:   []Platform{PlatformMacOS},
		Accelerator: Accelerator{Key: "k", Modifiers: []Modifier{ModCmdOrCtrl}},
	}
	second := first
	second.ID = "chat.search"
	second.CommandType = "chat.search"
	if err := registry.Register(first); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(second); !errors.Is(err, ErrConflict) {
		t.Fatalf("同平台同作用域冲突应失败，实际为 %v", err)
	}
}

func TestRegistryAllowsSameShortcutAcrossPlatformsAndScopes(t *testing.T) {
	registry := NewRegistry()
	macBinding := Binding{
		ID:          "mac.copy",
		CommandType: "copy",
		Scope:       "editor",
		Platforms:   []Platform{PlatformMacOS},
		Accelerator: Accelerator{Key: "c", Modifiers: []Modifier{ModCmdOrCtrl}},
	}
	windowsBinding := macBinding
	windowsBinding.ID = "windows.copy"
	windowsBinding.Platforms = []Platform{PlatformWindows}
	otherScope := macBinding
	otherScope.ID = "modal.copy"
	otherScope.Scope = "modal"
	for _, binding := range []Binding{macBinding, windowsBinding, otherScope} {
		if err := registry.Register(binding); err != nil {
			t.Fatalf("应允许跨平台或跨作用域并存: %v", err)
		}
	}
	if binding, err := registry.Resolve(PlatformWindows, "editor", Accelerator{Key: "c", Modifiers: []Modifier{ModCmdOrCtrl}}); err != nil || binding.ID != "windows.copy" {
		t.Fatalf("Windows 快捷键解析错误: %#v %v", binding, err)
	}
}

func TestAcceleratorSpeakEmptyAndArrowKeys(t *testing.T) {
	if got := (Accelerator{}).Speak(PlatformMacOS); got != "空" {
		t.Fatalf("空快捷键读法 = %s", got)
	}
	cases := []struct {
		key  string
		want string
	}{
		{key: "ArrowDown", want: "下箭头"},
		{key: "arrowup", want: "上箭头"},
		{key: "ArrowLeft", want: "左箭头"},
		{key: "ArrowRight", want: "右箭头"},
		{key: "Home", want: "行首"},
		{key: "End", want: "行尾"},
		{key: "Escape", want: "退出"},
		{key: "Tab", want: "制表符"},
		{key: "Enter", want: "回车"},
		{key: " ", want: "空格"},
		{key: "Delete", want: "删除"},
		{key: "PageUp", want: "上一页"},
		{key: "PageDown", want: "下一页"},
		{key: "F1", want: "F1"},
		{key: "Insert", want: "插入"},
		{key: "+", want: "加号"},
		{key: "-", want: "减号"},
		{key: "*", want: "星号"},
		{key: "=", want: "等号"},
		{key: "/", want: "斜杠"},
		{key: ",", want: "逗号"},
		{key: ".", want: "句号"},
		{key: "[", want: "左方括号"},
		{key: "]", want: "右方括号"},
		{key: ";", want: "分号"},
		{key: "'", want: "引号"},
		{key: "\\", want: "反斜杠"},
		{key: "`", want: "重音符"},
		{key: "{", want: "左花括号"},
		{key: "}", want: "右花括号"},
		{key: "(", want: "左圆括号"},
		{key: ")", want: "右圆括号"},
		{key: "|", want: "竖线"},
		{key: "?", want: "问号"},
		{key: "!", want: "感叹号"},
		{key: "@", want: "商业at"},
		{key: "#", want: "井号"},
		{key: "$", want: "美元符号"},
		{key: "%", want: "百分号"},
		{key: "^", want: "插入符号"},
		{key: "&", want: "和号"},
		{key: "<", want: "小于号"},
		{key: ">", want: "大于号"},
		{key: "CapsLock", want: "大写锁定"},
		{key: "ContextMenu", want: "菜单"},
		{key: "PrintScreen", want: "打印屏幕"},
		{key: "ScrollLock", want: "滚动锁定"},
		{key: "Pause", want: "暂停"},
		{key: "NumLock", want: "数字锁定"},
	}
	for _, tc := range cases {
		got := (Accelerator{Key: tc.key}).Speak(PlatformMacOS)
		if got != tc.want {
			t.Fatalf("%s 读法 = %s，期望 %s", tc.key, got, tc.want)
		}
	}
	got := (Accelerator{Key: "ArrowDown", Modifiers: []Modifier{ModCmdOrCtrl}}).Speak(PlatformMacOS)
	if got != "命令+下箭头" {
		t.Fatalf("组合方向键读法 = %s", got)
	}
}

func TestRegistryRejectsInvalidAndMissingBinding(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Binding{}); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("空绑定应失败，实际为 %v", err)
	}
	if err := registry.Register(Binding{
		ID:          "bad",
		CommandType: "bad",
		Scope:       "global",
		Platforms:   []Platform{"linux"},
		Accelerator: Accelerator{Key: "x"},
	}); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("非法平台应失败，实际为 %v", err)
	}
	if _, err := registry.Resolve(PlatformMacOS, "global", Accelerator{Key: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("缺失绑定应返回 ErrNotFound，实际为 %v", err)
	}
}
