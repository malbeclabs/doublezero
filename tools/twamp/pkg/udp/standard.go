package udp

import (
	"context"
	"fmt"
	"net"
)

type StandardDialer struct {
}

func NewStandardDialer() *StandardDialer {
	return &StandardDialer{}
}

func (d *StandardDialer) Dial(ctx context.Context, ifaceName string, localAddr, remoteAddr *net.UDPAddr) (*net.UDPConn, error) {
	if ifaceName != "" {
		_, err := net.InterfaceByName(ifaceName)
		if err != nil {
			return nil, fmt.Errorf("failed to dial: %w", err)
		}
	}
	dialer := net.Dialer{
		LocalAddr: localAddr,
	}
	conn, err := dialer.DialContext(ctx, "udp", remoteAddr.String())
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %w", err)
	}
	return conn.(*net.UDPConn), nil
}
