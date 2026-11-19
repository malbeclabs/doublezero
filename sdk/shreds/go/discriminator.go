package shreds

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

const discriminatorSize = 8

var (
	// The ::v2 suffix is part of the seed the feed subscription program hashes:
	// v2 replaced the per-month vault with a per-feed vault, which orphaned every
	// v1 account. A v1 account must therefore fail validation rather than decode
	// into a wrong collected amount.
	DiscriminatorFeedDistribution = sha256First8("dz::account::feed_distribution::v2")

	ErrInvalidDiscriminator = errors.New("invalid account discriminator")
)

func sha256First8(s string) [8]byte {
	h := sha256.Sum256([]byte(s))
	var disc [8]byte
	copy(disc[:], h[:8])
	return disc
}

func validateDiscriminator(data []byte, expected [8]byte) error {
	if len(data) < discriminatorSize {
		return fmt.Errorf("%w: data too short", ErrInvalidDiscriminator)
	}
	var got [8]byte
	copy(got[:], data[:8])
	if got != expected {
		return fmt.Errorf("%w: got %x, want %x", ErrInvalidDiscriminator, got, expected)
	}
	return nil
}
