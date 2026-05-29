// Package appflow orchestrates the GUI login flow without any UI dependency,
// so it can be unit-tested with fakes: login/register -> auto-register this
// device -> persist relay config -> start the subscriber.
package appflow

import (
	"github.com/qiyadeng/type4me/receiver/internal/config"
	"github.com/qiyadeng/type4me/receiver/internal/relayauth"
)

// AuthClient is the subset of relayauth.Client the controller needs.
type AuthClient interface {
	Login(username, password string) (relayauth.Session, error)
	Register(username, password, inviteCode string) (relayauth.Session, error)
	RegisterDevice(session, label, role string) (relayauth.Device, error)
}

// Controller drives the login/resume/logout flow.
type Controller struct {
	Cfg      *config.Config
	CfgPath  string
	Auth     AuthClient
	Hostname string // device label on first registration
	RelayURL string // build-baked relay URL written into config
	StartSub func(cfg *config.Config)
}

// ResumeIfConfigured starts the subscriber and returns true if the config is
// already a complete relay setup (no login needed); otherwise returns false.
func (c *Controller) ResumeIfConfigured() bool {
	if c.Cfg.IsRelayConfigured() {
		c.StartSub(c.Cfg)
		return true
	}
	return false
}

// LoginAndStart logs in (or registers), auto-registers this device if needed,
// persists the relay config, and starts the subscriber.
func (c *Controller) LoginAndStart(username, password, inviteCode string, register bool) error {
	var (
		sess relayauth.Session
		err  error
	)
	if register {
		sess, err = c.Auth.Register(username, password, inviteCode)
	} else {
		sess, err = c.Auth.Login(username, password)
	}
	if err != nil {
		return err
	}

	registeredNow := false
	if c.Cfg.DeviceToken == "" {
		dev, derr := c.Auth.RegisterDevice(sess.Token, c.Hostname, "either")
		if derr != nil {
			return derr
		}
		c.Cfg.DeviceID = dev.ID
		c.Cfg.DeviceToken = dev.Token
		registeredNow = true
	}

	oldMode, oldRelayURL := c.Cfg.Mode, c.Cfg.RelayURL
	c.Cfg.Mode = config.ModeRelaySubscriber
	c.Cfg.RelayURL = c.RelayURL
	if err := c.Cfg.Save(c.CfgPath); err != nil {
		c.Cfg.Mode = oldMode
		c.Cfg.RelayURL = oldRelayURL
		if registeredNow {
			c.Cfg.DeviceID = ""
			c.Cfg.DeviceToken = ""
		}
		return err
	}

	c.StartSub(c.Cfg)
	return nil
}

// Logout clears the device credentials and persists, so the next launch shows
// the login window. Stopping the running subscriber is the GUI's responsibility
// (it cancels the subscriber's context).
func (c *Controller) Logout() error {
	c.Cfg.DeviceID = ""
	c.Cfg.DeviceToken = ""
	return c.Cfg.Save(c.CfgPath)
}
