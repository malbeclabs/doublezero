package twamplight

import (
	"context"
	"crypto/ed25519"
	"net"
	"time"
)

// SignedReflectorVerifyInterval is the minimum time between signature
// verifications for the same public key. Packets arriving sooner are dropped
// without performing the expensive Ed25519 verify, bounding CPU cost from
// attackers who replay a known authorized pubkey with invalid signatures.
var SignedReflectorVerifyInterval = 55 * time.Second

// SignedReflector is used by the Probe to respond to passive probes.
type SignedReflector interface {
	Run(ctx context.Context) error
	Close() error
	LocalAddr() *net.UDPAddr
	SetAuthorizedKeys(keys [][32]byte)
}

// NewSignedReflector creates a new SignedReflector (Linux only).
func NewSignedReflector(addr string, timeout time.Duration, privateKey ed25519.PrivateKey, authorizedKeys [][32]byte) (SignedReflector, error) {
	return NewLinuxSignedReflector(addr, timeout, privateKey, authorizedKeys)
}
