package twamplight

import (
	"context"
	"log/slog"
	"net"
	"time"
)

var (
	defaultReadTimeout = 1 * time.Second
)

type Reflector interface {
	Run(ctx context.Context) error
	Close() error
	LocalAddr() *net.UDPAddr
}

func NewReflector(log *slog.Logger, addr string, timeout time.Duration) (Reflector, error) {
	reflector, err := NewLinuxReflector(addr, timeout)
	if err == ErrPlatformNotSupported {
		return NewBasicReflector(log, addr, timeout)
	}
	return reflector, err
}
