package fileopener

import (
	"path/filepath"
	"testing"
)

func TestCurrentReportsNativeOpenFailure(t *testing.T) {
	// open 进程能启动并不表示系统接受了文件；必须等待退出结果。
	if err := (Current{}).Open(filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Fatal("不存在的文件不能报告打开成功")
	}
}
