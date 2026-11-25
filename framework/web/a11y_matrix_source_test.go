package webassets

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var screenReaderEntry = regexp.MustCompile(`component:\s*'([^']+)'.*?file:\s*'([^']+)'.*?role:\s*'([^']+)'.*?nameAttribute:\s*'([^']+)'`)

func TestUIKitScreenReaderMatrixCoversExportedComponents(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	matrixSrc, err := os.ReadFile(filepath.Join(root, "a11y.ts"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	matches := screenReaderEntry.FindAllStringSubmatch(string(matrixSrc), -1)
	if len(matches) < 21 {
		t.Fatalf("屏幕阅读器矩阵条目过少: %d", len(matches))
	}
	if !strings.Contains(string(indexSrc), "screenReaderMatrix") || !strings.Contains(string(indexSrc), "TerminalSurface") {
		t.Fatal("UI Kit 入口必须导出 screenReaderMatrix 与 TerminalSurface")
	}
	for _, match := range matches {
		component, fileName, role, nameAttr := match[1], match[2], match[3], match[4]
		bodyBytes, err := os.ReadFile(filepath.Join(root, fileName))
		if err != nil {
			t.Fatalf("%s 缺少源文件 %s: %v", component, fileName, err)
		}
		body := string(bodyBytes)
		if !strings.Contains(body, nameAttr) {
			t.Fatalf("%s 缺少可访问名称 %s", component, nameAttr)
		}
		if !hasScreenReaderRole(body, role) {
			t.Fatalf("%s 缺少角色 %s", component, role)
		}
		if strings.Contains(match[0], "aria-roledescription") && !strings.Contains(body, "aria-roledescription") {
			t.Fatalf("%s 缺少 aria-roledescription", component)
		}
	}
}

func TestUIKitTerminalSurfaceAnnouncesStatus(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	source, err := os.ReadFile(filepath.Join(root, "TerminalSurface.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, want := range []string{`role="application"`, "aria-label", "aria-roledescription", "aria-live", "mzgui-live-region"} {
		if !strings.Contains(body, want) {
			t.Fatalf("TerminalSurface 缺少契约 %s", want)
		}
	}
}

func hasScreenReaderRole(body, role string) bool {
	switch role {
	case "navigation":
		return strings.Contains(body, "<nav") || strings.Contains(body, `role="navigation"`)
	case "native-select":
		return strings.Contains(body, "<select")
	case "checkbox":
		return strings.Contains(body, `type="checkbox"`)
	case "native-table":
		return strings.Contains(body, "<table")
	case "button":
		return strings.Contains(body, "<button") || strings.Contains(body, `role="button"`)
	case "textbox":
		return strings.Contains(body, "<input")
	case "group":
		return strings.Contains(body, `role="group"`)
	case "alert":
		return strings.Contains(body, `'alert'`) || strings.Contains(body, `role="alert"`)
	case "status":
		return strings.Contains(body, `role="status"`) || strings.Contains(body, `'status'`) || strings.Contains(body, `role={role}`)
	case "link":
		return strings.Contains(body, "<a ") || strings.Contains(body, "<a\n") || strings.Contains(body, "<a>")
	case "main":
		return strings.Contains(body, "<main")
	default:
		return strings.Contains(body, `role="`+role+`"`)
	}
}
