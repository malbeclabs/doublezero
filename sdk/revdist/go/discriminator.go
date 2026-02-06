package revdist

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

const discriminatorSize = 8

var (
	DiscriminatorProgramConfig          = sha256First8("dz::account::program_config")
	DiscriminatorDistribution           = sha256First8("dz::account::distribution")
	DiscriminatorSolanaValidatorDeposit = sha256First8("dz::account::solana_validator_deposit")
	DiscriminatorContributorRewards     = sha256First8("dz::account::contributor_rewards")
	DiscriminatorJournal                = sha256First8("dz::account::journal")

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
