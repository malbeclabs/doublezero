//go:build !linux
// +build !linux

package twamplight

import (
	"context"
	"crypto/ed25519"
	"net"
	"time"
)

func NewLinuxSignedSender(_ context.Context, _ string, _, _ *net.UDPAddr, _ ed25519.PrivateKey, _ [32]byte) (SignedSender, error) {
	return nil, ErrPlatformNotSupported
}

func NewLinuxSignedReflector(_ string, _ time.Duration, _ ed25519.PrivateKey, _ [][32]byte) (SignedReflector, error) {
	return nil, ErrPlatformNotSupported
}
