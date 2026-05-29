// Package relayauth is a small client for the relay's self-service account
// endpoints: login, register, and registering this machine as a device.
package relayauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client talks to a relay's /v1/auth/* and /v1/devices endpoints.
type Client struct {
	RelayURL   string
	HTTPClient *http.Client // defaults to a 10s-timeout client
}

// Session is the result of a login/register.
type Session struct {
	Token     string
	AccountID string
	Username  string
	ExpiresAt time.Time
}

// Device is the result of registering this machine.
type Device struct {
	ID    string
	Token string
	Label string
	Role  string
}

// APIError carries the relay's error code plus a human-readable message.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Message }

var defaultHTTPClient = &http.Client{Timeout: 10 * time.Second}

var errorMessages = map[string]string{
	"bad_json":              "请求格式错误",
	"username_invalid":      "用户名需为 3-32 个字符",
	"password_too_short":    "密码至少 8 个字符",
	"password_too_long":     "密码过长",
	"username_taken":        "用户名已被占用",
	"registration_disabled": "该服务未开放注册",
	"invalid_invite":        "邀请码无效",
	"invalid_credentials":   "用户名或密码错误",
	"invalid_session":       "登录已过期,请重新登录",
	"rate_limited":          "尝试过于频繁,请稍后再试",
	"account_not_found":     "账号不存在",
}

func messageFor(code string) string {
	if m, ok := errorMessages[code]; ok {
		return m
	}
	if code == "" {
		return "请求失败"
	}
	return "请求失败 (" + code + ")"
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return defaultHTTPClient
}

// do POSTs JSON to path (with optional bearer) and decodes a 2xx body into out.
func (c *Client) do(path, bearer string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.RelayURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
	var e struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&e)
	return &APIError{Status: resp.StatusCode, Code: e.Error, Message: messageFor(e.Error)}
}

type sessionResp struct {
	SessionToken string    `json:"session_token"`
	AccountID    string    `json:"account_id"`
	Username     string    `json:"username"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (c *Client) Login(username, password string) (Session, error) {
	var r sessionResp
	err := c.do("/v1/auth/login", "", map[string]string{
		"username": username, "password": password,
	}, &r)
	if err != nil {
		return Session{}, err
	}
	return Session{Token: r.SessionToken, AccountID: r.AccountID, Username: r.Username, ExpiresAt: r.ExpiresAt}, nil
}

func (c *Client) Register(username, password, inviteCode string) (Session, error) {
	var r sessionResp
	err := c.do("/v1/auth/register", "", map[string]string{
		"username": username, "password": password, "invite_code": inviteCode,
	}, &r)
	if err != nil {
		return Session{}, err
	}
	return Session{Token: r.SessionToken, AccountID: r.AccountID, Username: r.Username, ExpiresAt: r.ExpiresAt}, nil
}

// RegisterDevice registers this machine under the session's account.
func (c *Client) RegisterDevice(session, label, role string) (Device, error) {
	body := map[string]string{"label": label}
	if role != "" {
		body["role"] = role
	}
	var r struct {
		DeviceID    string `json:"device_id"`
		DeviceToken string `json:"device_token"`
		Label       string `json:"label"`
		Role        string `json:"role"`
	}
	if err := c.do("/v1/devices", session, body, &r); err != nil {
		return Device{}, err
	}
	if r.DeviceID == "" || r.DeviceToken == "" {
		return Device{}, fmt.Errorf("relay returned empty device id/token")
	}
	return Device{ID: r.DeviceID, Token: r.DeviceToken, Label: r.Label, Role: r.Role}, nil
}
