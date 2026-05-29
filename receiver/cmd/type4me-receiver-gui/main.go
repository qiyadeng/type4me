//go:build gui

package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/qiyadeng/type4me/receiver/internal/appflow"
	"github.com/qiyadeng/type4me/receiver/internal/config"
	"github.com/qiyadeng/type4me/receiver/internal/inject"
	"github.com/qiyadeng/type4me/receiver/internal/relay"
	"github.com/qiyadeng/type4me/receiver/internal/relayauth"
)

func main() {
	cfgPath := configPath()
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o700)

	cfg, err := config.ReadFile(cfgPath)
	if err != nil {
		cfg = &config.Config{Mode: config.ModeListener}
	}

	a := app.NewWithID("com.type4me.receiver")
	win := a.NewWindow("Type4Me 登录")

	statusItem := fyne.NewMenuItem("未连接", nil)
	// trayMenu is captured after creation so setStatus can call Refresh on it.
	var trayMenu *fyne.Menu
	setStatus := func(text string) {
		fyne.Do(func() {
			statusItem.Label = text
			if trayMenu != nil {
				trayMenu.Refresh()
			}
		})
	}

	inj := inject.NewPlatform()
	var (
		subMu     sync.Mutex
		cancelSub context.CancelFunc
	)
	stopSub := func() {
		subMu.Lock()
		if cancelSub != nil {
			cancelSub()
			cancelSub = nil
		}
		subMu.Unlock()
	}

	startSub := func(c *config.Config) {
		stopSub() // cancel any previously-running subscriber before starting a new one
		ctx, cancel := context.WithCancel(context.Background())
		subMu.Lock()
		cancelSub = cancel
		subMu.Unlock()
		sub := &relay.Subscriber{
			RelayURL:    c.RelayURL,
			DeviceToken: c.DeviceToken,
			Injector:    inj,
			HTTPClient:  &http.Client{Timeout: 0}, // SSE long-poll
			OnStatus: func(st relay.Status, _ error) {
				switch st {
				case relay.StatusConnecting:
					setStatus("连接中…")
				case relay.StatusConnected:
					setStatus("已连接")
				case relay.StatusReconnecting:
					setStatus("重连中…")
				case relay.StatusError:
					setStatus("连接失败")
				}
			},
		}
		go func() { _ = sub.Run(ctx) }()
	}

	ctrl := &appflow.Controller{
		Cfg:      cfg,
		CfgPath:  cfgPath,
		Auth:     &relayauth.Client{RelayURL: defaultRelayURL},
		Hostname: hostname(),
		RelayURL: defaultRelayURL,
		StartSub: startSub,
	}

	buildLoginForm(win, ctrl)

	if desk, ok := a.(desktop.App); ok {
		logoutItem := fyne.NewMenuItem("退出登录", func() {
			stopSub()
			_ = ctrl.Logout()
			setStatus("未连接")
			win.Show()
		})
		quitItem := fyne.NewMenuItem("退出", func() { a.Quit() })
		trayMenu = fyne.NewMenu("Type4Me", statusItem, logoutItem, quitItem)
		desk.SetSystemTrayMenu(trayMenu)
	}

	if ctrl.ResumeIfConfigured() {
		win.Hide() // already configured: live in the tray
	} else {
		win.Show()
	}
	a.Run()
}

// buildLoginForm wires the username/password/(invite) form to the controller.
func buildLoginForm(win fyne.Window, ctrl *appflow.Controller) {
	username := widget.NewEntry()
	username.SetPlaceHolder("用户名")
	password := widget.NewPasswordEntry()
	password.SetPlaceHolder("密码")
	invite := widget.NewEntry()
	invite.SetPlaceHolder("邀请码(仅注册时需要)")
	invite.Hide()

	registerMode := false
	errLabel := widget.NewLabel("")
	errLabel.Wrapping = fyne.TextWrapWord

	submit := widget.NewButton("登录", nil)
	toggle := widget.NewButton("没有账号?去注册", nil)

	applyMode := func() {
		if registerMode {
			invite.Show()
			submit.SetText("注册")
			toggle.SetText("已有账号?去登录")
		} else {
			invite.Hide()
			submit.SetText("登录")
			toggle.SetText("没有账号?去注册")
		}
	}
	toggle.OnTapped = func() {
		registerMode = !registerMode
		applyMode()
	}
	submit.OnTapped = func() {
		errLabel.SetText("")
		submit.Disable()
		go func() {
			err := ctrl.LoginAndStart(username.Text, password.Text, invite.Text, registerMode)
			fyne.Do(func() {
				submit.Enable()
				if err != nil {
					errLabel.SetText(err.Error())
					return
				}
				win.Hide()
			})
		}()
	}
	applyMode()

	win.SetContent(container.NewVBox(username, password, invite, submit, toggle, errLabel))
	win.Resize(fyne.NewSize(320, 240))
	win.SetCloseIntercept(func() { win.Hide() }) // closing the window keeps the tray alive
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
