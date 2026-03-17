package signed

import (
	"context"
	"log/slog"
	"time"
)

// Reflector is used by the Probe to respond to passive probes.
type Reflector interface {
	Run(ctx context.Context) error
	Close() error
	Port() uint16
	SetAuthorizedKeys(keys [][32]byte)
	SetOffsets(offsets [][]byte)
	SetLogger(logger *slog.Logger)
}

// NewReflector creates a signed TWAMP reflector. Only the port in addr is used; any IP is ignored.
// verifyInterval is the minimum time between probe pairs for the same public key.
// Each sender is allowed 2 probes per window; additional probes are dropped.
func NewReflector(addr string, timeout time.Duration, signer Signer, geoprobePubkey [32]byte, authorizedKeys [][32]byte, verifyInterval time.Duration) (Reflector, error) {
	return NewLinuxReflector(addr, timeout, signer, geoprobePubkey, authorizedKeys, verifyInterval)
}
