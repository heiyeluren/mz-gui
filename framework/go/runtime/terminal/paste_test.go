package terminal

import (
	"errors"
	"strings"
	"testing"
)

func TestEncodeFilePathsPOSIXMatchesLegacyContract(t *testing.T) {
	value, err := EncodeFilePaths([]string{"/tmp/plain", "/tmp/with space.txt", "/tmp/it's-safe"}, ShellPOSIX)
	if err != nil {
		t.Fatal(err)
	}
	if value != "'/tmp/plain' '/tmp/with space.txt' '/tmp/it'\\''s-safe' " {
		t.Fatalf("POSIX 路径编码不一致: %q", value)
	}
}

func TestEncodeFilePathsPOSIXEscapesControlCharacters(t *testing.T) {
	value, err := EncodeFilePaths([]string{"/tmp/a\x1bb\rc\nd\te\x00f"}, ShellPOSIX)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`\x1b`, `\r`, `\n`, `\t`, `\x00`} {
		if !strings.Contains(value, expected) {
			t.Fatalf("缺少控制字符转义 %q: %q", expected, value)
		}
	}
	if strings.ContainsAny(value, "\x1b\r\n\t\x00") {
		t.Fatalf("编码结果不得保留原始控制字符: %q", value)
	}
}

func TestEncodeFilePathsPowerShellQuotesApostrophes(t *testing.T) {
	value, err := EncodeFilePaths([]string{`C:\Program Files\Mozi`, `C:\Users\O'Brien\a.txt`}, ShellPowerShell)
	if err != nil {
		t.Fatal(err)
	}
	if value != `'C:\Program Files\Mozi' 'C:\Users\O''Brien\a.txt' ` {
		t.Fatalf("PowerShell 路径编码不一致: %q", value)
	}
	if _, err := EncodeFilePaths([]string{"C:\\tmp\\a\n.txt"}, ShellPowerShell); !errors.Is(err, ErrUnsupportedPath) {
		t.Fatalf("Windows 控制字符路径必须拒绝: %v", err)
	}
}

func TestEncodeFilePathsEnforcesBounds(t *testing.T) {
	if _, err := EncodeFilePaths(nil, ShellPOSIX); !errors.Is(err, ErrNoPastePaths) {
		t.Fatalf("空路径错误不一致: %v", err)
	}
	if _, err := EncodeFilePaths([]string{""}, ShellPOSIX); !errors.Is(err, ErrEmptyPastePath) {
		t.Fatalf("空元素错误不一致: %v", err)
	}
	tooMany := make([]string, MaximumPastePathCount+1)
	for index := range tooMany {
		tooMany[index] = "/tmp/a"
	}
	if _, err := EncodeFilePaths(tooMany, ShellPOSIX); !errors.Is(err, ErrTooManyPastePaths) {
		t.Fatalf("路径数量错误不一致: %v", err)
	}
	if _, err := EncodeFilePaths([]string{strings.Repeat("a", MaximumPastePathBytes+1)}, ShellPOSIX); !errors.Is(err, ErrPastePathsTooLarge) {
		t.Fatalf("路径总量错误不一致: %v", err)
	}
}
