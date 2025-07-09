//go:build !linux
// +build !linux

package udp

import (
	"context"
	"fmt"
	"net"
)

var (
	ErrPlatformNotSupported = fmt.Errorf("not supported on this platform")
)

func NewKernelDialer() (*KernelDialer, error) {
	return nil, ErrPlatformNotSupported
}

type KernelDialer struct{}

func (d *KernelDialer) Dial(ctx context.Context, ifaceName string, localAddr, remoteAddr *net.UDPAddr) (*net.UDPConn, error) {
	return nil, ErrPlatformNotSupported
}
