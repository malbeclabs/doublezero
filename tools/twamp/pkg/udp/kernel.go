//go:build !linux
// +build !linux

package udp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"
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

func NewKernelTimestampedReader(_ *slog.Logger, _ *net.UDPConn) (*KernelTimestampedReader, error) {
	return nil, ErrPlatformNotSupported
}

type KernelTimestampedReader struct{}

func (c *KernelTimestampedReader) Now() time.Time {
	return time.Time{}
}

func (c *KernelTimestampedReader) Read(ctx context.Context, buf []byte) (int, time.Time, error) {
	return 0, time.Time{}, ErrPlatformNotSupported
}
