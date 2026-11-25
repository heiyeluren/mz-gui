package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTerminalRepliesDoNotClaimUserInput(t *testing.T) {
	device := newInputTestDevice()
	manager, created := newInputTestManager(t, device)
	replies := "\x1b[?1;2c\x1b]11;rgb:1818/1c1c/2020\x1b\\\x1b[I\x1b[O"
	if err := manager.Write(context.Background(), created.SessionID, []byte(replies)); err != nil {
		t.Fatal(err)
	}
	info, _ := manager.Get(context.Background(), created.SessionID)
	if info.InputRevision != created.InputRevision {
		t.Fatal("终端协议应答被当成用户接管", info.InputRevision)
	}
	if _, err := manager.WriteIfInputUnchanged(context.Background(), created.SessionID, created.InputRevision, []byte("任务\r")); err != nil {
		t.Fatal(err)
	}
	if device.input() != replies+"任务\r" {
		t.Fatal("协议应答或任务没有完整写入设备")
	}
}

func TestTerminalReplyMixedWithUserInputStillBlocksAutomaticInput(t *testing.T) {
	for _, value := range []string{"\x1b[Ihello", "hello\x1b[O", "\x1b[I\r", "\x1b[O\x03", "\x1b[?1;2cx", "x\x1b[?1;2c", "\x03", "\x1b[1;2R", "\x1b]11;rgb:1818/1c1c/2020\a\r", "\x1b]52;c;dGV4dA==\a"} {
		t.Run(value, func(t *testing.T) {
			device := newInputTestDevice()
			manager, created := newInputTestManager(t, device)
			if err := manager.Write(context.Background(), created.SessionID, []byte(value)); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.WriteIfInputUnchanged(context.Background(), created.SessionID, created.InputRevision, []byte("不得发送")); !errors.Is(err, ErrInputChanged) {
				t.Fatal("普通输入被误排除", err)
			}
			if device.input() != value {
				t.Fatal("用户输入被改写")
			}
		})
	}
}

func TestConditionalProtocolBytesStillConsumeTheirAttempt(t *testing.T) {
	device := newInputTestDevice()
	manager, created := newInputTestManager(t, device)
	if revision, err := manager.WriteIfInputUnchanged(context.Background(), created.SessionID, 0, []byte("\x1b[?1;2c")); err != nil || revision != 1 {
		t.Fatalf("显式条件写仍应消费本次版本: revision=%d err=%v", revision, err)
	}
}

func TestTerminalNavigationKeepsSeparateInputProvenance(t *testing.T) {
	for _, input := range []string{"\r", "\x1b[A\x1b[B\r", "text", "\x03", "\x1b[200~\r\x1b[201~", "\rtext"} {
		t.Run(input, func(t *testing.T) {
			device := newInputTestDevice()
			manager, created := newInputTestManager(t, device)
			if err := manager.Write(context.Background(), created.SessionID, []byte(input)); err != nil {
				t.Fatal(err)
			}
			info, _ := manager.Get(context.Background(), created.SessionID)
			if info.InputRevision != 1 {
				t.Fatal("导航仍必须计入普通输入版本")
			}
			wantSubmit := uint64(0)
			if strings.ContainsAny(input, "\r\n") {
				wantSubmit = 1
			}
			if info.LastSubmitInputRevision != wantSubmit {
				t.Fatal("提交与导航未区分", info.LastSubmitInputRevision)
			}
			if info.LastSubmitAt.IsZero() != (wantSubmit == 0) {
				t.Fatal("仅提交输入才记录时间，不将导航或普通编辑当作提交")
			}
			navigation := input == "\r" || input == "\x1b[A\x1b[B\r"
			if navigation != (info.LastNonNavigationInputRevision == 0) {
				t.Fatal("输入来源分类错误", info.LastNonNavigationInputRevision)
			}
			if _, err := manager.WriteIfInputUnchanged(context.Background(), created.SessionID, 0, []byte("不得覆盖")); !errors.Is(err, ErrInputChanged) {
				t.Fatal("普通条件写不能绕过导航输入")
			}
		})
	}
}

func TestNavigationDoesNotEraseEarlierTextInput(t *testing.T) {
	device := newInputTestDevice()
	manager, created := newInputTestManager(t, device)
	if err := manager.Write(context.Background(), created.SessionID, []byte("text")); err != nil {
		t.Fatal(err)
	}
	if err := manager.Write(context.Background(), created.SessionID, []byte("\r")); err != nil {
		t.Fatal(err)
	}
	info, _ := manager.Get(context.Background(), created.SessionID)
	if info.InputRevision != 2 || info.LastNonNavigationInputRevision != 1 {
		t.Fatal("导航清除了先前输入事实", info)
	}
}
