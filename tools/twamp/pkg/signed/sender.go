package signed

import (
	"context"
	"net"
	"time"
)

// Sender is used by the Target to initiate passive probing.
type Sender interface {
	Probe(ctx context.Context) (time.Duration, *ReplyPacket, error)
	Close() error
}

// NewSender creates a signed TWAMP sender. Only localAddr.Port is used; any IP is ignored.
func NewSender(ctx context.Context, iface string, localAddr, remoteAddr *net.UDPAddr, signer Signer, remotePubkey [32]byte) (Sender, error) {
	return NewLinuxSender(ctx, iface, localAddr, remoteAddr, signer, remotePubkey)
}
