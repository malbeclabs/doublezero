package shreds

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

const discriminatorSize = 8

var (
	DiscriminatorProgramConfig              = sha256First8("dz::account::program_config")
	DiscriminatorExecutionController        = sha256First8("dz::account::execution_controller")
	DiscriminatorClientSeat                 = sha256First8("dz::account::client_seat")
	DiscriminatorPaymentEscrow              = sha256First8("dz::account::payment_escrow")
	DiscriminatorShredDistribution          = sha256First8("dz::account::shred_distribution")
	DiscriminatorValidatorClientRewards     = sha256First8("dz::account::validator_client_rewards")
	DiscriminatorInstantSeatAllocationRequest = sha256First8("dz::account::instant_seat_allocation_request")
	DiscriminatorWithdrawSeatRequest        = sha256First8("dz::account::withdraw_seat_request")
	DiscriminatorMetroHistory               = sha256First8("dz::account::metro_history")
	DiscriminatorDeviceHistory              = sha256First8("dz::account::device_history")

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
