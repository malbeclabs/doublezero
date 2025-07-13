//go:build !linux
// +build !linux

package twamplight

import (
	"net"
	"time"
)

func NewLinuxSender(iface string, localAddr, remoteAddr *net.UDPAddr) (Sender, error) {
	return nil, ErrPlatformNotSupported
}

func NewLinuxReflector(port uint16, timeout time.Duration) (Reflector, error) {
	return nil, ErrPlatformNotSupported
}
