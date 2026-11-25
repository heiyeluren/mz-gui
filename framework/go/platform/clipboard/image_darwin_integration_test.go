//go:build darwin && cgo

package clipboard

import (
	"os"
	"testing"
)

func TestDarwinClipboardImageIntegration(t *testing.T) {
	if os.Getenv("MZ_GUI_TEST_SYSTEM_CLIPBOARD") != "1" {
		t.Skip("仅在显式启用时读取真实系统剪贴板")
	}
	result, err := ReadImage(DefaultMaxImageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Data) == 0 || result.MIMEType != "image/png" || result.Width < 1 || result.Height < 1 {
		t.Fatalf("系统剪贴板图片结果无效: %#v", result)
	}
}
