package twamplight

import "errors"

var (
	ErrInvalidPacket        = errors.New("invalid packet format")
	ErrPlatformNotSupported = errors.New("platform not supported")
)
