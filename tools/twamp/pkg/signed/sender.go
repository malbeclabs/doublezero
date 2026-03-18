package signed

import (
	"context"
	"log/slog"
	"net"
	"time"
)

// ProbePairResult holds the results of a paired probe exchange.
type ProbePairResult struct {
	RTT0   time.Duration
	RTT1   time.Duration
	Reply0 *ReplyPacket
	Reply1 *ReplyPacket
}

// Sender is used by the Target to initiate passive probing.
type Sender interface {
	Probe(ctx context.Context) (time.Duration, *ReplyPacket, error)
	ProbePair(ctx context.Context) (ProbePairResult, error)
	SetLogger(logger *slog.Logger)
	Close() error
}

// NewSender creates a signed TWAMP sender. Only localAddr.Port is used; any IP is ignored.
func NewSender(ctx context.Context, iface string, localAddr, remoteAddr *net.UDPAddr, signer Signer, remotePubkey [32]byte) (Sender, error) {
	return NewLinuxSender(ctx, iface, localAddr, remoteAddr, signer, remotePubkey)
}
