package xterm

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func engine(t *testing.T) *Engine {
	t.Helper()
	node, err := exec.LookPath("node")
	if explicit := os.Getenv("MZ_GUI_TEST_TERMINAL_NODE"); explicit != "" {
		node = explicit
		err = nil
	}
	if err != nil {
		t.Fatal("验收需要 Node", err)
	}
	e, err := New(node, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}
func TestEngineLargeTUI(t *testing.T) {
	e := engine(t)
	e.Write([]byte("\x1b[1;1H标题必须保留"))
	chunk := strings.Repeat("\x1b[24;1H"+strings.Repeat(" ", 70)+"\x1b[24;1H正在处理", 300)
	s := time.Now()
	for i := 0; i < 200; i++ {
		if err := e.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	v, err := e.Serialize()
	if err != nil || !strings.Contains(v, "标题必须保留") {
		t.Fatalf("%v %q", err, v)
	}
	t.Log("elapsed", time.Since(s), "bytes", 200*len(chunk))
}
func TestRestoreContinuesSameState(t *testing.T) {
	a, b := engine(t), engine(t)
	prefix := "\x1b[2J\x1b[1;1H标题\x1b[3;10r\x1b[?6h\x1b[31m\x1b[2;1H内容\x1b7\x1b[?2004h\x1b[?1003h\x1b[?1006h\x1b[38;2;"
	if err := a.Write([]byte(prefix)); err != nil {
		t.Fatal(err)
	}
	s, err := a.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Restore(s); err != nil {
		t.Fatal(err)
	}
	next := []byte("10;20;30m继续\x1b8再次\r\n末尾")
	a.Write(next)
	b.Write(next)
	av, _ := a.Serialize()
	bv, _ := b.Serialize()
	var aa, bb any
	json.Unmarshal([]byte(av), &aa)
	json.Unmarshal([]byte(bv), &bb)
	ax, _ := json.Marshal(aa)
	bx, _ := json.Marshal(bb)
	if string(ax) != string(bx) {
		t.Fatalf("恢复后差异\n%s\n%s", ax, bx)
	}
}
func TestWorkerCrashRecoversWithoutPTY(t *testing.T) {
	a := engine(t)
	a.Write([]byte("保留标题\r\n"))
	a.Serialize()
	a.Write([]byte("未检查点的输出"))
	a.cmd.Process.Kill()
	if err := a.Write([]byte("继续")); err != nil {
		t.Fatal(err)
	}
	s, _ := a.Serialize()
	if !strings.Contains(s, "保留标题") || !strings.Contains(s, "未检查点的输出继续") {
		t.Fatal(s)
	}
}

func TestRecoveryMatrix(t *testing.T) {
	cases := []struct{ name, prefix, next string }{
		{"超链接与下划线", "\x1b]8;;https://example.com\x07链接\x1b]8;;\x07\x1b[4:3m样式", "继续\x1b[0m正常"},
		{"同步输出", "\x1b[?2026h半帧", "完成\x1b[?2026l"},
		{"调色板", "\x1b]4;1;#112233\x07\x1b]11;#445566\x07\x1b[31m颜色", "后续"},
		{"组合字符分片", "e", "́继续"},
		{"表情分片", "👩‍", "💻继续"},
		{"自动换行", strings.Repeat("x", 80), "下一行"},
		{"中文宽字符", "标题\r\n" + strings.Repeat("x", 79) + "中文🙂é", "继续\r\n末尾"},
		{"主备用屏幕", "主屏\x1b7\x1b[?1049h\x1b[2J\x1b[1;1H备用屏\x1b[4;4H", "继续\x1b[?1049l恢复主屏\x1b8落点"},
		{"滚动区域", "\x1b[2;5r\x1b[?6h\x1b[3;1H标题", "\n一\n二\n三\n四"},
		{"保存光标及样式", "\x1b[31m\x1b[3;5H保存\x1b7\x1b[32m\x1b[8;2H当前", "\x1b8保存位置\x1b[39m正常"},
		{"自定义制表位", "\x1b[3g\x1b[1;5H\x1bH\x1b[1;12H\x1bH\x1b[1;1H", "\t甲\t乙"},
		{"字符集", "\x1b(0lqq", "qqk\x1b(B原文"},
		{"重复字符", "a", "\x1b[6b末尾"},
		{"分片颜色", "\x1b[38;2;", "10;20;30m颜色"},
		{"分片标题", "\x1b]0;当前标", "题\x07正文"},
		{"分片结束标记", "\x1b]0;标题\x1b", "\\正文"},
		{"隐藏光标及输入模式", "\x1b[?25l\x1b[?2004h\x1b[?1004h\x1b[?1h\x1b[4h", "输入\x1b[?25h"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := engine(t), engine(t)
			if err := a.Write([]byte(tc.prefix)); err != nil {
				t.Fatal(err)
			}
			s, err := a.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if err = b.Restore(s); err != nil {
				t.Fatal(err)
			}
			a.Write([]byte(tc.next))
			b.Write([]byte(tc.next))
			av, _ := a.Serialize()
			bv, _ := b.Serialize()
			var aa, bb any
			json.Unmarshal([]byte(av), &aa)
			json.Unmarshal([]byte(bv), &bb)
			ax, _ := json.Marshal(aa)
			bx, _ := json.Marshal(bb)
			if string(ax) != string(bx) {
				t.Fatalf("恢复后状态不同\n%s\n%s", ax, bx)
			}
		})
	}
}
func TestFreezeReleasesProcessButKeepsState(t *testing.T) {
	e := engine(t)
	e.Write([]byte("已结束\r\n"))
	if err := e.Freeze(); err != nil {
		t.Fatal(err)
	}
	if e.cmd != nil {
		t.Fatal("历史终端残留状态进程")
	}
	s, err := e.Serialize()
	if err != nil || !strings.Contains(s, "已结束") {
		t.Fatal(err, s)
	}
}
func TestRecoveryDoesNotRepeatAlreadyAnsweredQueries(t *testing.T) {
	e := engine(t)
	e.Write([]byte("\x1b[6n"))
	if string(e.TakeReplies()) != "\x1b[1;1R" {
		t.Fatal("缺少第一次应答")
	}
	e.cmd.Process.Kill()
	if err := e.Write([]byte("\x1b[6n")); err != nil {
		t.Fatal(err)
	}
	if string(e.TakeReplies()) != "\x1b[1;1R" {
		t.Fatal("恢复重复了旧查询应答")
	}
}
func TestResizeRestoreKeepsSavedCursorForLaterExpansion(t *testing.T) {
	a, b := engine(t), engine(t)
	a.Write([]byte("\x1b[22;70H保存\x1b7\x1b[1;1H当前"))
	a.Resize(40, 12)
	s, err := a.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Restore(s); err != nil {
		t.Fatal(err)
	}
	a.Resize(80, 24)
	b.Resize(80, 24)
	a.Write([]byte("\x1b8恢复"))
	b.Write([]byte("\x1b8恢复"))
	av, _ := a.Serialize()
	bv, _ := b.Serialize()
	var aa, bb any
	json.Unmarshal([]byte(av), &aa)
	json.Unmarshal([]byte(bv), &bb)
	ax, _ := json.Marshal(aa)
	bx, _ := json.Marshal(bb)
	if string(ax) != string(bx) {
		t.Fatalf("缩小恢复再放大丢失保存光标\n%s\n%s", ax, bx)
	}
}
func TestWorkerIgnoresNodeOptionsAndTreatsOutputAsData(t *testing.T) {
	t.Setenv("NODE_OPTIONS", "--not-a-valid-runtime-option")
	e := engine(t)
	input := "\n{\"id\":100,\"op\":\"unknown\"}\nprocess.exit(88)\n"
	if err := e.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	state, err := e.Serialize()
	if err != nil || !strings.Contains(state, "process.exit(88)") {
		t.Fatal("输出被当成指令", err)
	}
}
func TestInvalidRestoreDoesNotClearExistingScreen(t *testing.T) {
	e := engine(t)
	e.Write([]byte("必须保留"))
	before, err := e.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	json.Unmarshal([]byte(before), &value)
	value["attributes"] = nil
	bad, _ := json.Marshal(value)
	if e.Restore(string(bad)) == nil {
		t.Fatal("接受了非法快照")
	}
	after, err := e.Serialize()
	if err != nil || after != before {
		t.Fatal("非法恢复破坏当前画面", err)
	}
}
func TestBundledWorkerRuntime(t *testing.T) {
	node := os.Getenv("MZ_GUI_TEST_TERMINAL_NODE")
	if node == "" {
		t.Skip("仅发行资源专项显式指定实际 helper")
	}
	e, err := New(node, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.Write([]byte("发行资源终端\r\n"))
	state, err := e.Serialize()
	if err != nil || !strings.Contains(state, "发行资源终端") {
		t.Fatal(err)
	}
}

// 调色板覆盖、重置、快照恢复与进程恢复均保留调用方指定的默认颜色。
func TestDefaultColorsSurviveResetAndRecovery(t *testing.T) {
	e := engine(t)
	if err := e.SetDefaultColors("#252721", "#e6e2d9", "#4e356e"); err != nil {
		t.Fatal(err)
	}
	query := func(want string) {
		t.Helper()
		if err := e.Write([]byte("\x1b]11;?\x07")); err != nil {
			t.Fatal(err)
		}
		if got := string(e.TakeReplies()); !strings.Contains(got, want) {
			t.Fatalf("背景应答 %q，期望 %q", got, want)
		}
	}
	query("rgb:e6e6/e2e2/d9d9")
	e.Write([]byte("\x1b]11;#123456\x07"))
	query("rgb:1212/3434/5656")
	e.Write([]byte("\x1b]111\x07"))
	query("rgb:e6e6/e2e2/d9d9")
	snapshot, err := e.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	query("rgb:e6e6/e2e2/d9d9")
	e.cmd.Process.Kill()
	query("rgb:e6e6/e2e2/d9d9")
	e.Write([]byte("\x1bc"))
	query("rgb:e6e6/e2e2/d9d9")
	// 老快照没有颜色字段时继续使用会话配置，不退回硬编码深色。
	var legacy map[string]any
	if err := json.Unmarshal([]byte(snapshot), &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "defaultColors")
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Restore(string(data)); err != nil {
		t.Fatal(err)
	}
	query("rgb:e6e6/e2e2/d9d9")
}
