package wailsapp

import (
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestControllerTracksValidatedCloseBehavior(t *testing.T) {
	controller := NewController(CloseToTray)
	if controller.CloseBehavior() != CloseToTray || !controller.ShouldHideOnClose() {
		t.Fatalf("default=%q", controller.CloseBehavior())
	}
	if action := controller.CloseAction(); action != CloseActionHide {
		t.Fatalf("托盘策略的关闭动作=%q", action)
	}

	controller.SetCloseBehavior(CloseQuit)
	if controller.ShouldHideOnClose() {
		t.Fatal("退出策略不得隐藏窗口")
	}
	if action := controller.CloseAction(); action != CloseActionQuit {
		t.Fatalf("退出策略的关闭动作=%q", action)
	}

	controller.SetCloseBehavior("invalid")
	if controller.CloseBehavior() != CloseQuit {
		t.Fatal("无效关闭策略不得覆盖当前值")
	}
}

func TestControllerBeginsQuitOnlyOnce(t *testing.T) {
	controller := NewController(CloseQuit)
	if !controller.beginQuit() {
		t.Fatal("首次退出请求必须取得执行权")
	}
	if controller.beginQuit() {
		t.Fatal("重复退出请求不得再次执行 app.Quit")
	}
	if controller.ShouldHideOnClose() {
		t.Fatal("退出开始后不得再回退到托盘隐藏")
	}
}

func TestApplyWindowCloseHidesOnTrayAndQuitsOnce(t *testing.T) {
	tray := NewController(CloseToTray)
	var trayCalls []string
	tray.ApplyWindowClose(
		func() { trayCalls = append(trayCalls, "cancel") },
		func() { trayCalls = append(trayCalls, "hide") },
		func() { trayCalls = append(trayCalls, "quit") },
	)
	if strings.Join(trayCalls, ",") != "cancel,hide" {
		t.Fatalf("tray 关窗口动作=%v", trayCalls)
	}

	quitting := NewController(CloseQuit)
	var quitCalls []string
	quitting.ApplyWindowClose(
		func() { quitCalls = append(quitCalls, "cancel") },
		func() { quitCalls = append(quitCalls, "hide") },
		func() { quitCalls = append(quitCalls, "quit") },
	)
	quitting.ApplyWindowClose(
		func() { quitCalls = append(quitCalls, "cancel") },
		func() { quitCalls = append(quitCalls, "hide") },
		func() { quitCalls = append(quitCalls, "quit") },
	)
	if strings.Join(quitCalls, ",") != "cancel,quit" {
		t.Fatalf("quit 关窗口动作=%v", quitCalls)
	}
}

func TestActivateWindowRestoresShowsAndFocusesInOrder(t *testing.T) {
	var calls []string
	if !ActivateWindow(WindowActivation{
		Restore: func() { calls = append(calls, "restore") },
		Show:    func() { calls = append(calls, "show") },
		Focus:   func() { calls = append(calls, "focus") },
	}) {
		t.Fatal("完整窗口激活动作应成功")
	}
	want := []string{"restore", "show", "focus"}
	if len(calls) != len(want) {
		t.Fatalf("窗口激活动作=%v", calls)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("窗口激活动作顺序=%v", calls)
		}
	}
	if ActivateWindow(WindowActivation{}) {
		t.Fatal("缺少任一原生动作时不得伪装成激活成功")
	}
}

func TestActivateWindowVerifiesVisible(t *testing.T) {
	var calls []string
	if !ActivateWindow(WindowActivation{
		Restore: func() { calls = append(calls, "restore") },
		Show:    func() { calls = append(calls, "show") },
		Focus:   func() { calls = append(calls, "focus") },
		Visible: func() bool {
			calls = append(calls, "visible")
			return true
		},
	}) {
		t.Fatal("窗口真实可见时应判定激活成功")
	}
	want := []string{"restore", "show", "focus", "visible"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("窗口激活顺序=%v", calls)
	}
}

func TestActivateWindowRetriesWhenFirstVisibilityCheckFails(t *testing.T) {
	var calls []string
	visibleChecks := 0
	if !ActivateWindow(WindowActivation{
		Restore: func() { calls = append(calls, "restore") },
		Show:    func() { calls = append(calls, "show") },
		Focus:   func() { calls = append(calls, "focus") },
		Visible: func() bool {
			visibleChecks++
			calls = append(calls, "visible")
			return visibleChecks == 2
		},
	}) {
		t.Fatal("第二次可见性确认通过时应判定激活成功")
	}
	want := []string{"restore", "show", "focus", "visible", "show", "focus", "visible"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("窗口重试激活顺序=%v", calls)
	}
}

func TestActivateWindowDoesNotClaimSuccessWhenWindowStaysInvisible(t *testing.T) {
	var calls []string
	if ActivateWindow(WindowActivation{
		Restore: func() { calls = append(calls, "restore") },
		Show:    func() { calls = append(calls, "show") },
		Focus:   func() { calls = append(calls, "focus") },
		Visible: func() bool {
			calls = append(calls, "visible")
			return false
		},
	}) {
		t.Fatal("窗口两次仍不可见时不得判定激活成功")
	}
	want := []string{"restore", "show", "focus", "visible", "show", "focus", "visible"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("窗口失败激活顺序=%v", calls)
	}
}

type activationTestWindow struct {
	minimised    bool
	maximised    bool
	fullscreen   bool
	visible      bool
	focused      bool
	showCalls    int
	visibleAfter int
}

// Restore 模拟 Wails 的还原语义：还原最大化或全屏会改变用户窗口布局。
func (window *activationTestWindow) Restore() {
	if window.minimised {
		window.minimised = false
	} else if window.fullscreen {
		window.fullscreen = false
	} else if window.maximised {
		window.maximised = false
	}
}

func (window *activationTestWindow) UnMinimise() { window.minimised = false }
func (window *activationTestWindow) Show() application.Window {
	window.showCalls++
	window.visible = window.showCalls >= window.visibleAfter
	return nil
}
func (window *activationTestWindow) Focus()          { window.focused = true }
func (window *activationTestWindow) IsVisible() bool { return window.visible }

func TestRevealWindowPreservesPresentationAcrossRepeatedActivation(t *testing.T) {
	for _, test := range []struct {
		name   string
		window activationTestWindow
	}{
		{name: "普通", window: activationTestWindow{visible: true}},
		{name: "最大化", window: activationTestWindow{maximised: true, visible: true}},
		{name: "全屏", window: activationTestWindow{fullscreen: true, visible: true}},
		{name: "隐藏或遮挡的最大化", window: activationTestWindow{maximised: true}},
		{name: "隐藏或遮挡的全屏", window: activationTestWindow{fullscreen: true}},
		{name: "最小化", window: activationTestWindow{minimised: true}},
		{name: "最大化后最小化", window: activationTestWindow{minimised: true, maximised: true}},
		{name: "可见性延迟", window: activationTestWindow{maximised: true, visibleAfter: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			window := test.window
			for attempt := 1; attempt <= 2; attempt++ {
				if !revealWindow(&window) {
					t.Fatalf("第 %d 次唤起未成功", attempt)
				}
				if window.minimised || !window.visible || !window.focused {
					t.Fatalf("第 %d 次唤起没有显示并聚焦窗口：%+v", attempt, window)
				}
				if window.maximised != test.window.maximised || window.fullscreen != test.window.fullscreen {
					t.Fatalf("第 %d 次唤起改变了最大化或全屏状态：%+v", attempt, window)
				}
			}
			wantShows := 2
			if test.window.visibleAfter == 2 {
				wantShows++
			}
			if window.showCalls != wantShows {
				t.Fatalf("显示次数=%d，期望=%d", window.showCalls, wantShows)
			}
		})
	}
}

func TestApplicationReopenIsOnlyBoundOnMacOS(t *testing.T) {
	if !supportsApplicationReopen("darwin") {
		t.Fatal("macOS 必须支持从 Dock 重新显示隐藏窗口")
	}
	if supportsApplicationReopen("windows") || supportsApplicationReopen("linux") {
		t.Fatal("不得在其他平台伪造 macOS Dock reopen 能力")
	}
}

func TestApplicationMenuIsOnlyBoundOnMacOS(t *testing.T) {
	if !supportsApplicationMenu("darwin") {
		t.Fatal("macOS 必须绑定系统菜单栏")
	}
	if supportsApplicationMenu("windows") || supportsApplicationMenu("linux") {
		t.Fatal("Windows 不得展示 Edit/Window 原生菜单栏")
	}
}

func TestBindOptionsUseBrandNeutralDefaults(t *testing.T) {
	options := (BindOptions{}).normalized()
	if options.ShowWindowLabel != "显示窗口" || options.QuitLabel != "退出" || options.Tooltip != "应用" {
		t.Fatalf("defaults=%#v", options)
	}
}

func TestControllerBindsWailsMenuTrayAndCloseHook(t *testing.T) {
	app := application.New(application.Options{Name: "mz-gui-framework-lifecycle-test"})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "main"})
	controller := NewController(CloseToTray)
	controller.Bind(app, window, BindOptions{
		ShowWindowLabel: "显示测试窗口",
		QuitLabel:       "退出测试应用",
		Tooltip:         "框架测试",
		Icon:            []byte("icon"),
	})
}
