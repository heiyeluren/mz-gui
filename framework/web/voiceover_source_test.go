package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitVoiceOverPhrasesCoverPrimarySurfaces(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	voiceover, err := os.ReadFile(filepath.Join(root, "voiceover.ts"))
	if err != nil {
		t.Fatal(err)
	}
	focus, err := os.ReadFile(filepath.Join(root, "focus.ts"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	spoken := string(voiceover)
	for _, want := range []string{"speakVoiceOver", "voiceOverRoleDescription", "speakVoiceOverState", "speakVoiceOverProgress", "speakVoiceOverSort", "speakVoiceOverCount", "voiceOverCountText", "announceRestore", "对话框", "导航", "应用程序", "编辑文本", "百分之", "升序", "共", "voiceOverPhrases", "screenReaderMatrix"} {
		if !strings.Contains(spoken, want) {
			t.Fatalf("VoiceOver 契约缺少 %s", want)
		}
	}
	a11y, err := os.ReadFile(filepath.Join(root, "a11y.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range []string{"TextField", "FeedbackToast", "SegmentedControl"} {
		if !strings.Contains(string(a11y), "component: '"+component+"'") {
			t.Fatalf("屏幕阅读器矩阵缺少 %s", component)
		}
	}
	if !strings.Contains(string(focus), "trapFocus") || !strings.Contains(string(focus), "getTabbableElements") {
		t.Fatal("焦点陷阱契约不完整")
	}
	if !strings.Contains(string(indexSrc), "speakVoiceOver") || !strings.Contains(string(indexSrc), "trapFocus") {
		t.Fatal("UI Kit 入口必须导出 VoiceOver 与焦点陷阱")
	}
}
