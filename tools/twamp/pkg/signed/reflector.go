package signed

import (
	"context"
	"time"
)

// VerifyInterval is the minimum time between signature verifications for the
// same public key. Packets arriving sooner are dropped without performing the
// expensive Ed25519 verify, bounding CPU cost from attackers who replay a
// known authorized pubkey with invalid signatures.
var VerifyInterval = 55 * time.Second

// Reflector is used by the Probe to respond to passive probes.
type Reflector interface {
	Run(ctx context.Context) error
	Close() error
	Port() uint16
	SetAuthorizedKeys(keys [][32]byte)
}

// NewReflector creates a signed TWAMP reflector. Only the port in addr is used; any IP is ignored.
func NewReflector(addr string, timeout time.Duration, signer Signer, geoprobePubkey [32]byte, authorizedKeys [][32]byte) (Reflector, error) {
	return NewLinuxReflector(addr, timeout, signer, geoprobePubkey, authorizedKeys)
}
