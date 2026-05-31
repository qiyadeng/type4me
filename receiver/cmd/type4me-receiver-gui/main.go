//go:build gui

package main

import (
	"context"
	_ "embed"
	"image/color"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/qiyadeng/type4me/receiver/internal/appflow"
	"github.com/qiyadeng/type4me/receiver/internal/config"
	"github.com/qiyadeng/type4me/receiver/internal/inject"
	"github.com/qiyadeng/type4me/receiver/internal/relay"
	"github.com/qiyadeng/type4me/receiver/internal/relayauth"
)

//go:embed assets/icon.png
var iconBytes []byte

func main() {
	cfgPath := configPath()
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o700)

	cfg, err := config.ReadFile(cfgPath)
	if err != nil {
		cfg = &config.Config{Mode: config.ModeListener}
	}

	a := app.NewWithID("com.type4me.receiver")
	a.Settings().SetTheme(type4meTheme{})
	icon := fyne.NewStaticResource("type4me.png", iconBytes)
	a.SetIcon(icon)

	win := a.NewWindow("Type4Me")
	win.SetIcon(icon)
	win.Resize(fyne.NewSize(400, 520))
	win.CenterOnScreen()

	ui := &gui{app: a, win: win, icon: icon}
	ui.inj = inject.NewPlatform()
	ui.ctrl = &appflow.Controller{
		Cfg:      cfg,
		CfgPath:  cfgPath,
		Auth:     &relayauth.Client{RelayURL: defaultRelayURL},
		Hostname: hostname(),
		RelayURL: defaultRelayURL,
		StartSub: ui.startSub,
	}

	ui.buildTray()
	win.SetCloseIntercept(func() { win.Hide() }) // close button → live in tray

	if ui.ctrl.ResumeIfConfigured() {
		ui.showStatus()
		win.Hide() // already configured: resume in tray, reopen via tray "显示窗口"
	} else {
		ui.showLogin()
		win.Show()
	}
	a.Run()
}

// gui holds the shared UI/runtime state for the receiver window + tray.
type gui struct {
	app  fyne.App
	win  fyne.Window
	icon fyne.Resource
	inj  inject.Injector
	ctrl *appflow.Controller

	subMu     sync.Mutex
	cancelSub context.CancelFunc

	status     relay.Status
	statusItem *fyne.MenuItem
	trayMenu   *fyne.Menu

	// live status-view widgets; non-nil only while the status view is shown.
	stMu       sync.Mutex
	statusDot  *canvas.Text
	statusText *canvas.Text
}

// ---- subscriber lifecycle ----

func (g *gui) startSub(c *config.Config) {
	g.stopSub()
	ctx, cancel := context.WithCancel(context.Background())
	g.subMu.Lock()
	g.cancelSub = cancel
	g.subMu.Unlock()
	sub := &relay.Subscriber{
		RelayURL:    c.RelayURL,
		DeviceToken: c.DeviceToken,
		Injector:    g.inj,
		HTTPClient:  &http.Client{Timeout: 0}, // SSE long-poll
		OnStatus:    func(st relay.Status, _ error) { g.setStatus(st) },
	}
	go func() { _ = sub.Run(ctx) }()
}

func (g *gui) stopSub() {
	g.subMu.Lock()
	if g.cancelSub != nil {
		g.cancelSub()
		g.cancelSub = nil
	}
	g.subMu.Unlock()
}

// ---- status text/color mapping ----

func statusLabel(st relay.Status) (string, color.Color) {
	switch st {
	case relay.StatusConnecting:
		return "连接中…", color.NRGBA{0xC7, 0x8C, 0x26, 0xFF}
	case relay.StatusConnected:
		return "已连接", color.NRGBA{0x4C, 0x9E, 0x59, 0xFF}
	case relay.StatusReconnecting:
		return "重连中…", color.NRGBA{0xC7, 0x8C, 0x26, 0xFF}
	case relay.StatusError:
		return "连接失败", color.NRGBA{0xCC, 0x47, 0x38, 0xFF}
	default:
		return "未连接", color.NRGBA{0x9A, 0x95, 0x8C, 0xFF}
	}
}

func (g *gui) setStatus(st relay.Status) {
	g.status = st
	text, col := statusLabel(st)
	fyne.Do(func() {
		if g.statusItem != nil {
			g.statusItem.Label = "状态:" + text
			if g.trayMenu != nil {
				g.trayMenu.Refresh()
			}
		}
		g.stMu.Lock()
		if g.statusText != nil {
			g.statusText.Text = text
			g.statusText.Color = col
			g.statusText.Refresh()
		}
		if g.statusDot != nil {
			g.statusDot.Color = col
			g.statusDot.Refresh()
		}
		g.stMu.Unlock()
	})
}

// ---- tray ----

func (g *gui) buildTray() {
	desk, ok := g.app.(desktop.App)
	if !ok {
		return
	}
	text, _ := statusLabel(g.status)
	g.statusItem = fyne.NewMenuItem("状态:"+text, nil)
	g.statusItem.Disabled = true

	openItem := fyne.NewMenuItem("显示窗口", func() {
		g.win.Show()
		g.win.RequestFocus()
	})
	logoutItem := fyne.NewMenuItem("退出登录", func() { g.logout() })
	quitItem := fyne.NewMenuItem("退出", func() { g.stopSub(); g.app.Quit() })

	g.trayMenu = fyne.NewMenu("Type4Me", g.statusItem, fyne.NewMenuItemSeparator(),
		openItem, logoutItem, quitItem)
	desk.SetSystemTrayMenu(g.trayMenu)
}

func (g *gui) logout() {
	g.stopSub()
	_ = g.ctrl.Logout()
	g.setStatus("")
	g.showLogin()
	g.win.Show()
	g.win.RequestFocus()
}

// ---- shared header ----

func (g *gui) header(subtitle string) fyne.CanvasObject {
	img := canvas.NewImageFromResource(g.icon)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(64, 64))

	title := canvas.NewText("Type4Me", color.NRGBA{0x1F, 0x1D, 0x1B, 0xFF})
	title.TextSize = 22
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	sub := canvas.NewText(subtitle, color.NRGBA{0x6B, 0x66, 0x5D, 0xFF})
	sub.TextSize = 13
	sub.Alignment = fyne.TextAlignCenter

	return container.NewVBox(
		container.NewCenter(img),
		title,
		sub,
	)
}

func fieldLabel(text string) *widget.Label {
	return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

// ---- login view ----

func (g *gui) showLogin() {
	g.stMu.Lock()
	g.statusText, g.statusDot = nil, nil
	g.stMu.Unlock()

	username := widget.NewEntry()
	username.SetPlaceHolder("你的用户名")
	password := widget.NewPasswordEntry()
	password.SetPlaceHolder("至少 8 位")
	invite := widget.NewEntry()
	invite.SetPlaceHolder("注册才需要")

	inviteLabel := fieldLabel("邀请码")
	inviteLabel.Hide()
	invite.Hide()

	errText := canvas.NewText("", color.NRGBA{0xCC, 0x47, 0x38, 0xFF})
	errText.TextSize = 13
	progress := widget.NewProgressBarInfinite()
	progress.Hide()

	submit := widget.NewButton("登录", nil)
	submit.Importance = widget.HighImportance
	toggle := widget.NewButton("没有账号?去注册", nil)
	toggle.Importance = widget.LowImportance

	registerMode := false
	applyMode := func() {
		if registerMode {
			inviteLabel.Show()
			invite.Show()
			submit.SetText("注册")
			toggle.SetText("已有账号?去登录")
		} else {
			inviteLabel.Hide()
			invite.Hide()
			submit.SetText("登录")
			toggle.SetText("没有账号?去注册")
		}
	}
	toggle.OnTapped = func() { registerMode = !registerMode; applyMode() }

	submit.OnTapped = func() {
		errText.Text = ""
		errText.Refresh()
		submit.Disable()
		toggle.Disable()
		progress.Show()
		reg := registerMode
		go func() {
			err := g.ctrl.LoginAndStart(username.Text, password.Text, invite.Text, reg)
			fyne.Do(func() {
				progress.Hide()
				submit.Enable()
				toggle.Enable()
				if err != nil {
					errText.Text = err.Error()
					errText.Color = color.NRGBA{0xCC, 0x47, 0x38, 0xFF}
					errText.Refresh()
					return
				}
				g.showStatus() // logged in → show the status panel
			})
		}()
	}
	applyMode()

	form := container.NewVBox(
		fieldLabel("用户名"), username,
		fieldLabel("密码"), password,
		inviteLabel, invite,
		errText,
		submit,
		progress,
		container.NewCenter(toggle),
	)
	g.win.SetTitle("Type4Me 登录")
	g.win.SetContent(container.NewBorder(
		g.header("登录以连接你的设备"), nil, nil, nil,
		container.New(layout.NewCustomPaddedLayout(12, 12, 18, 18), form),
	))
}

// ---- status (logged-in) view ----

func (g *gui) showStatus() {
	text, col := statusLabel(g.status)
	dot := canvas.NewText("●", col)
	dot.TextSize = 16
	st := canvas.NewText(text, col)
	st.TextSize = 16
	st.TextStyle = fyne.TextStyle{Bold: true}

	g.stMu.Lock()
	g.statusDot, g.statusText = dot, st
	g.stMu.Unlock()

	statusRow := container.NewHBox(dot, st)

	infoCard := container.NewVBox(
		infoRow("设备", hostname()),
		infoRow("服务器", defaultRelayURL),
	)

	hint := canvas.NewText("转写文本会注入到本机当前焦点窗口。", color.NRGBA{0x6B, 0x66, 0x5D, 0xFF})
	hint.TextSize = 12

	logout := widget.NewButton("退出登录", func() { g.logout() })
	logout.Importance = widget.DangerImportance
	hide := widget.NewButton("隐藏到托盘", func() { g.win.Hide() })

	body := container.NewVBox(
		container.NewCenter(statusRow),
		widget.NewSeparator(),
		infoCard,
		hint,
		layout.NewSpacer(),
		container.NewGridWithColumns(2, hide, logout),
	)

	g.win.SetTitle("Type4Me")
	g.win.SetContent(container.NewBorder(
		g.header("已登录"), nil, nil, nil,
		container.New(layout.NewCustomPaddedLayout(12, 12, 18, 18), body),
	))
}

func infoRow(label, value string) fyne.CanvasObject {
	l := canvas.NewText(label, color.NRGBA{0x6B, 0x66, 0x5D, 0xFF})
	l.TextSize = 13
	v := canvas.NewText(value, color.NRGBA{0x1F, 0x1D, 0x1B, 0xFF})
	v.TextSize = 13
	v.Alignment = fyne.TextAlignTrailing
	return container.NewBorder(nil, nil, l, nil, container.NewHBox(layout.NewSpacer(), v))
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "type4me-receiver"
}

func configPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "type4me-receiver", "config.json")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "type4me-receiver", "config.json")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "type4me-receiver", "config.json")
	}
}
