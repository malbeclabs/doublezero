//go:build !linux
// +build !linux

package twamplight

import (
	"context"
	"log/slog"
	"net"
	"time"
)

func NewLinuxSender(ctx context.Context, log *slog.Logger, iface string, localAddr, remoteAddr *net.UDPAddr) (Sender, error) {
	return nil, ErrPlatformNotSupported
}

func NewLinuxReflector(addr string, timeout time.Duration) (Reflector, error) {
	return nil, ErrPlatformNotSupported
}
