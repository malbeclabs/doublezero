package udp

import (
	"context"
	"log/slog"
	"net"
)

type Dialer interface {
	Dial(ctx context.Context, ifaceName string, localAddr, remoteAddr *net.UDPAddr) (*net.UDPConn, error)
}

func NewDialer(log *slog.Logger) Dialer {
	kt, err := NewKernelDialer()
	if err == nil {
		log.Debug("Using kernel dialer")
		return kt
	}
	log.Debug("Falling back to standard dialer", "error", err)
	return NewStandardDialer()
}
