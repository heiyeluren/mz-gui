package wailsapp

import (
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	CloseToTray = "tray"
	CloseQuit   = "quit"
)

type CloseAction string

const (
	CloseActionHide CloseAction = "hide"
	CloseActionQuit CloseAction = "quit"
)

type WindowActivation struct {
	// Restore 仅解除最小化；不得取消窗口已有的最大化或全屏状态。
	Restore func()
	Show    func()
	Focus   func()
	Visible func() bool
}

type BindOptions struct {
	ShowWindowLabel string
	QuitLabel       string
	Tooltip         string
	Icon            []byte
}

func (options BindOptions) normalized() BindOptions {
	if options.ShowWindowLabel == "" {
		options.ShowWindowLabel = "显示窗口"
	}
	if options.QuitLabel == "" {
		options.QuitLabel = "退出"
	}
	if options.Tooltip == "" {
		options.Tooltip = "应用"
	}
	return options
}

type Controller struct {
	closeBehavior atomic.Value
	quitting      atomic.Bool
	windowMu      sync.RWMutex
	window        *application.WebviewWindow
}

func NewController(defaultCloseBehavior string) *Controller {
	controller := &Controller{}
	if !validCloseBehavior(defaultCloseBehavior) {
		defaultCloseBehavior = CloseToTray
	}
	controller.closeBehavior.Store(defaultCloseBehavior)
	return controller
}

func (controller *Controller) SetCloseBehavior(value string) {
	if validCloseBehavior(value) {
		controller.closeBehavior.Store(value)
	}
}

func (controller *Controller) CloseBehavior() string {
	value, _ := controller.closeBehavior.Load().(string)
	if !validCloseBehavior(value) {
		return CloseToTray
	}
	return value
}

func (controller *Controller) ShouldHideOnClose() bool {
	return !controller.quitting.Load() && controller.CloseBehavior() == CloseToTray
}

func (controller *Controller) CloseAction() CloseAction {
	if controller.ShouldHideOnClose() {
		return CloseActionHide
	}
	return CloseActionQuit
}

func (controller *Controller) beginQuit() bool {
	return controller.quitting.CompareAndSwap(false, true)
}

// ApplyWindowClose 把关窗口动作落到隐藏托盘或真正退出，退出只执行一次。
func (controller *Controller) ApplyWindowClose(cancel, hide, quit func()) {
	if controller == nil {
		return
	}
	switch controller.CloseAction() {
	case CloseActionHide:
		if cancel != nil {
			cancel()
		}
		if hide != nil {
			hide()
		}
	case CloseActionQuit:
		if !controller.beginQuit() {
			return
		}
		if cancel != nil {
			cancel()
		}
		if quit != nil {
			quit()
		}
	}
}

// ActivateWindow 按跨平台一致顺序解除最小化、显示并聚焦窗口。
func ActivateWindow(actions WindowActivation) bool {
	if actions.Restore == nil || actions.Show == nil || actions.Focus == nil {
		return false
	}
	actions.Restore()
	actions.Show()
	actions.Focus()
	if actions.Visible == nil || actions.Visible() {
		return true
	}
	actions.Show()
	actions.Focus()
	if actions.Visible != nil && !actions.Visible() {
		return false
	}
	return true
}

// RevealWindow 使用 Wails 原生窗口能力唤起窗口，保留最大化与全屏状态。
func RevealWindow(window *application.WebviewWindow) bool {
	if window == nil {
		return false
	}
	return revealWindow(window)
}

// revealWindowTarget 只暴露窗口唤起所需的原生操作，便于独立验证窗口状态。
type revealWindowTarget interface {
	UnMinimise()
	Show() application.Window
	Focus()
	IsVisible() bool
}

func revealWindow(window revealWindowTarget) bool {
	return ActivateWindow(WindowActivation{
		// Wails Restore 会退出最大化或全屏，唤起时只能解除最小化。
		Restore: window.UnMinimise,
		Show:    func() { window.Show() },
		Focus:   window.Focus,
		Visible: window.IsVisible,
	})
}

// RevealWindow 激活已绑定的主窗口。
func (controller *Controller) RevealWindow() bool {
	if controller == nil {
		return false
	}
	controller.windowMu.RLock()
	window := controller.window
	controller.windowMu.RUnlock()
	return RevealWindow(window)
}

// RevealWindowIfHidden 仅在主窗口不可见时恢复窗口，用于 macOS 应用重新激活。
func (controller *Controller) RevealWindowIfHidden() bool {
	if controller == nil {
		return false
	}
	controller.windowMu.RLock()
	window := controller.window
	controller.windowMu.RUnlock()
	if window == nil {
		return false
	}
	if window.IsVisible() {
		return true
	}
	return RevealWindow(window)
}

// Bind 将关闭策略、系统托盘和（仅 macOS）应用菜单绑定到 Wails 主窗口。
func (controller *Controller) Bind(app *application.App, window *application.WebviewWindow, options BindOptions) {
	if controller == nil || app == nil || window == nil {
		return
	}
	options = options.normalized()
	controller.windowMu.Lock()
	controller.window = window
	controller.windowMu.Unlock()
	app.OnShutdown(func() { controller.quitting.Store(true) })
	if supportsApplicationReopen(runtime.GOOS) {
		app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
			controller.RevealWindow()
		})
		app.Event.OnApplicationEvent(events.Mac.ApplicationDidBecomeActive, func(*application.ApplicationEvent) {
			controller.RevealWindowIfHidden()
		})
	}
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		controller.ApplyWindowClose(event.Cancel, func() { window.Hide() }, app.Quit)
	})

	bindApplicationMenu(app)

	trayMenu := application.NewMenu()
	trayMenu.Add(options.ShowWindowLabel).OnClick(func(*application.Context) { controller.RevealWindow() })
	trayMenu.AddSeparator()
	trayMenu.Add(options.QuitLabel).OnClick(func(*application.Context) {
		if controller.beginQuit() {
			app.Quit()
		}
	})
	tray := app.SystemTray.New().SetMenu(trayMenu).AttachWindow(window).OnClick(func() { controller.RevealWindow() })
	tray.SetTooltip(options.Tooltip)
	if len(options.Icon) > 0 {
		tray.SetIcon(options.Icon)
	}
}

func validCloseBehavior(value string) bool {
	return value == CloseToTray || value == CloseQuit
}

func supportsApplicationReopen(goos string) bool {
	return goos == "darwin"
}

// bindApplicationMenu 只在 macOS 系统菜单栏挂 App/Edit/Window；
// Windows 会把同一套 role 画成窗口内菜单栏，不绑定。
func bindApplicationMenu(app *application.App) {
	if app == nil || !supportsApplicationMenu(runtime.GOOS) {
		return
	}
	applicationMenu := app.NewMenu()
	applicationMenu.AddRole(application.AppMenu)
	applicationMenu.AddRole(application.EditMenu)
	applicationMenu.AddRole(application.WindowMenu)
	app.Menu.Set(applicationMenu)
}

func supportsApplicationMenu(goos string) bool {
	return goos == "darwin"
}
