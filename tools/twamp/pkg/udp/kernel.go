//go:build linux
// +build linux

package udp

import (
	"context"
	"fmt"
	"net"
	"syscall"
)

type KernelDialer struct{}

func NewKernelDialer() (*KernelDialer, error) {
	return &KernelDialer{}, nil
}

func (d *KernelDialer) Dial(ctx context.Context, ifaceName string, localAddr, remoteAddr *net.UDPAddr) (*net.UDPConn, error) {
	dialer := net.Dialer{
		LocalAddr: localAddr,
		Control: func(network, address string, c syscall.RawConn) error {
			var controlErr error
			err := c.Control(func(fd uintptr) {
				controlErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, ifaceName)
			})
			if err != nil {
				return fmt.Errorf("failed to set socket option: %w", err)
			}
			return controlErr
		},
	}

	conn, err := dialer.DialContext(ctx, "udp", remoteAddr.String())
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %w", err)
	}

	return conn.(*net.UDPConn), nil
}
