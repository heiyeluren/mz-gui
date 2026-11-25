package terminal

import (
	"bytes"
	"strings"
	"testing"
)

func TestScreenProjectsCurrentCellsAndDrainsDeviceQueries(t *testing.T) {
	screen, err := NewScreen(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer screen.Close()
	data := []byte("\x1b[?2004h\x1b[6n\x1b]11;?\a\x1b[2J\x1b[1;1HTrust this folder?\x1b[2J\x1b[1;1H中文\x1b[10;1H│ >       │\x1b[10;5H")
	for _, part := range [][]byte{data[:20], data[20:]} {
		if err := screen.Feed(part, 80, 24); err != nil {
			t.Fatal(err)
		}
	}
	value := screen.Snapshot()
	if strings.Contains(strings.Join(value.Lines, "\n"), "Trust this folder") || !strings.HasPrefix(value.Lines[0], "中文") || value.CursorY != 9 || value.CursorX != 4 {
		t.Fatalf("屏幕未按VT控制序列更新: %#v", value)
	}
	if err := screen.Feed(bytes.Repeat([]byte("a"), (1<<20)+1), 80, 24); err == nil {
		t.Fatal("接受了过量屏幕数据")
	}
	if err := screen.Feed(nil, 10000, 24); err == nil {
		t.Fatal("接受了过大屏幕")
	}
}
