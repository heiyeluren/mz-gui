package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndependentFrameworkLayoutAndShowcaseIdentity(t *testing.T) {
	root := repositoryRoot(t)
	for _, relativePath := range []string{
		"framework/go/core",
		"framework/go/lifecycle",
		"framework/go/transport",
		"framework/go/platform",
		"framework/go/storage",
		"framework/go/security",
		"framework/ui-kit/src/index.ts",
		"framework/web/tokens.css",
		"gen/go/mzgui/framework/bootstrap/v1/bootstrap.pb.go",
		"examples/framework-showcase/main.go",
		"examples/framework-showcase/frontend/index.html",
	} {
		if _, err := os.Stat(filepath.Join(root, relativePath)); err != nil {
			t.Errorf("独立公共框架缺少 %s: %v", relativePath, err)
		}
	}
	for _, relativePath := range []string{
		"examples/framework-showcase/mzgui.app.toml",
		"examples/framework-showcase/frontend/index.html",
	} {
		content, err := os.ReadFile(filepath.Join(root, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "黑夜路人的 APP") {
			t.Errorf("Showcase 必须使用独立产品标题: %s", relativePath)
		}
	}
	generated, err := os.ReadFile(filepath.Join(root, "examples", "framework-showcase", "generated", "appmeta", "manifest_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	agentdDeclaration := ""
	for _, line := range strings.Split(string(generated), "\n") {
		if strings.Contains(line, "AgentdProcess") {
			agentdDeclaration = strings.Join(strings.Fields(line), " ")
			break
		}
	}
	if agentdDeclaration != `AgentdProcess = ""` {
		t.Fatalf("默认 Showcase 必须保持单进程，不得隐式声明 agentd: %s", agentdDeclaration)
	}
}
