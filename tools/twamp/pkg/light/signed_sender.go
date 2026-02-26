package twamplight

import (
	"context"
	"crypto/ed25519"
	"net"
	"time"
)

// SignedSender is used by the Target to initiate passive probing.
type SignedSender interface {
	Probe(ctx context.Context) (time.Duration, *SignedReplyPacket, error)
	Close() error
	LocalAddr() *net.UDPAddr
}

// NewSignedSender creates a new SignedSender (Linux only).
func NewSignedSender(ctx context.Context, iface string, localAddr, remoteAddr *net.UDPAddr, privateKey ed25519.PrivateKey, remotePubkey [32]byte) (SignedSender, error) {
	return NewLinuxSignedSender(ctx, iface, localAddr, remoteAddr, privateKey, remotePubkey)
}
