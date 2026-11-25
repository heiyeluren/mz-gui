package terminal

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestTerminalLocaleFallbackHonorsEffectiveEnvironment(t *testing.T) {
	for _, test := range []struct {
		name, platform  string
		base            []string
		overrides, want map[string]string
	}{
		{"macOS GUI 缺省", "darwin", nil, nil, map[string]string{"LC_CTYPE": "UTF-8"}},
		{"Linux GUI 缺省", "linux", nil, nil, map[string]string{"LC_CTYPE": "C.UTF-8"}},
		{"Windows 保持原行为", "windows", nil, nil, map[string]string{}},
		{"继承 LANG", "darwin", []string{"LANG=zh_CN.UTF-8"}, nil, map[string]string{"LANG": "zh_CN.UTF-8"}},
		{"显式字符编码", "darwin", []string{"LC_CTYPE=en_US.ISO8859-1"}, nil, map[string]string{"LC_CTYPE": "en_US.ISO8859-1"}},
		{"显式 C 不覆盖", "darwin", []string{"LC_ALL=C"}, nil, map[string]string{"LC_ALL": "C"}},
		{"请求覆盖父环境", "linux", []string{"LC_ALL=C", "LANG=en_US.UTF-8"}, map[string]string{"LC_ALL": "ja_JP.UTF-8"}, map[string]string{"LC_ALL": "ja_JP.UTF-8", "LANG": "en_US.UTF-8"}},
		{"空覆盖后补默认", "darwin", []string{"LANG=en_US.UTF-8", "LC_CTYPE=en_US.UTF-8"}, map[string]string{"LANG": "", "LC_CTYPE": "", "LC_ALL": ""}, map[string]string{"LANG": "", "LC_CTYPE": "UTF-8", "LC_ALL": ""}},
		{"消息语言不决定字符编码", "darwin", []string{"LC_MESSAGES=zh_CN.UTF-8"}, nil, map[string]string{"LC_MESSAGES": "zh_CN.UTF-8", "LC_CTYPE": "UTF-8"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			for _, entry := range terminalProgramEnvironment(test.platform, test.base, test.overrides) {
				name, value, _ := strings.Cut(entry, "=")
				if _, exists := got[name]; exists {
					t.Fatalf("重复变量 %s", name)
				}
				got[name] = value
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("环境=%v want=%v", got, test.want)
			}
		})
	}
}

func TestTerminalLocaleDoesNotMutateCallerOrOtherLaunches(t *testing.T) {
	base := []string{"PATH=/runtime-a/bin", "RUNTIME_ROOT=/runtime-a"}
	before := append([]string(nil), base...)
	overrides := map[string]string{"LC_CTYPE": ""}
	parent := os.Environ()
	a := terminalProgramEnvironment("darwin", base, overrides)
	b := terminalProgramEnvironment("darwin", []string{"PATH=/runtime-b/bin"}, map[string]string{"LC_ALL": "C"})
	if !reflect.DeepEqual(base, before) || !reflect.DeepEqual(overrides, map[string]string{"LC_CTYPE": ""}) || !reflect.DeepEqual(parent, os.Environ()) {
		t.Fatal("修改了调用方或父进程环境")
	}
	if !strings.Contains(strings.Join(a, "\n"), "LC_CTYPE=UTF-8") || strings.Contains(strings.Join(b, "\n"), "UTF-8") || strings.Contains(strings.Join(b, "\n"), "runtime-a") {
		t.Fatalf("不同启动之间串环境: a=%v b=%v", a, b)
	}
}
