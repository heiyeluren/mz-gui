package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinPackagesDisableAppleDoubleFiles(t *testing.T) {
	for _, name := range []string{"package-showcase-darwin.sh", "package-agentd-showcase-darwin.sh"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "scripts", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "ditto -c -k --norsrc --noextattr --noqtn --keepParent") {
				t.Fatalf("Darwin 打包必须禁用资源、扩展属性和隔离元数据: %s", path)
			}
		})
	}
}
