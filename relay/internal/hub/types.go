package hub

import "time"

// Role describes whether a device can send, receive, or both.
// v1 stores it but treats all devices as "either".
type Role string

const (
	RoleSender   Role = "sender"
	RoleReceiver Role = "receiver"
	RoleEither   Role = "either"
)

type Account struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Device struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	Label     string    `json:"label"`
	Role      Role      `json:"role"`
	TokenHash string    `json:"token_hash"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

type Message struct {
	ID                string    `json:"id"`
	Text              string    `json:"text"`
	FromDevice        string    `json:"from_device"`
	RequestID         string    `json:"request_id"`
	PreserveClipboard bool      `json:"preserve_clipboard"`
	CreatedAt         time.Time `json:"created_at"`
}
