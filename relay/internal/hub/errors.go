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
)
