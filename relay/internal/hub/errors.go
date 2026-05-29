package hub

import "errors"

var (
	ErrAccountNotFound     = errors.New("account not found")
	ErrDeviceNotFound      = errors.New("device not found")
	ErrCrossAccount        = errors.New("target device not in sender's account")
	ErrReceiverOffline     = errors.New("receiver offline")
	ErrBackpressure        = errors.New("receiver backpressure")
	ErrInvalidToken        = errors.New("invalid token")
	ErrAccountNameRequired = errors.New("account name required")

	ErrUsernameInvalid    = errors.New("username must be 3-32 characters")
	ErrPasswordTooShort   = errors.New("password too short")
	ErrPasswordTooLong    = errors.New("password too long")
	ErrUsernameTaken      = errors.New("username already taken")
	ErrInvalidCredentials = errors.New("invalid credentials")
)
