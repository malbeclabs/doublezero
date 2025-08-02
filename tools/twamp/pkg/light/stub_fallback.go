//go:build !linux
// +build !linux

package twamplight

import (
	"context"
	"net"
	"time"
)

func NewLinuxSender(ctx context.Context, iface string, localAddr, remoteAddr *net.UDPAddr) (Sender, error) {
	return nil, ErrPlatformNotSupported
}

func NewLinuxReflector(addr string, timeout time.Duration) (Reflector, error) {
	return nil, ErrPlatformNotSupported
}
