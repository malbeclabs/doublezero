package twamplight

import (
	"context"
	"log/slog"
	"net"
	"time"
)

var (
	defaultProbeTimeout = 1 * time.Second
)

type Sender interface {
	Probe(ctx context.Context) (time.Duration, error)
	Close() error
	LocalAddr() *net.UDPAddr
}

func NewSender(ctx context.Context, log *slog.Logger, iface string, localAddr, remoteAddr *net.UDPAddr) (Sender, error) {
	sender, err := NewLinuxSender(ctx, iface, localAddr, remoteAddr)
	if err == ErrPlatformNotSupported {
		return NewBasicSender(ctx, log, iface, localAddr, remoteAddr)
	}
	return sender, err
}
