//go:build !linux
// +build !linux

package signed

import (
	"context"
	"errors"
	"net"
	"time"
)

var errPlatformNotSupported = errors.New("platform not supported")

func NewLinuxSender(_ context.Context, _ string, _, _ *net.UDPAddr, _ Signer, _ [32]byte) (Sender, error) {
	return nil, errPlatformNotSupported
}

func NewLinuxReflector(_ string, _ time.Duration, _ Signer, _ [][32]byte) (Reflector, error) {
	return nil, errPlatformNotSupported
}
