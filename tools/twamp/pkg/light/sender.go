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

type SenderConfig struct {
	Logger            *slog.Logger
	LocalInterface    string
	LocalAddr         *net.UDPAddr
	RemoteAddr        *net.UDPAddr
	SchedulerPriority *int
	PinToCPU          *int
}

func NewSender(ctx context.Context, cfg SenderConfig) (Sender, error) {
	sender, err := NewLinuxSender(ctx, cfg)
	if err == ErrPlatformNotSupported {
		return NewBasicSender(ctx, cfg)
	}
	return sender, err
}
