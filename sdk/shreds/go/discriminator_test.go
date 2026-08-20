package shreds

import (
	"fmt"
	"testing"
)

func TestDiscriminatorsAreUnique(t *testing.T) {
	discs := map[string][8]byte{
		"ProgramConfig":                DiscriminatorProgramConfig,
		"ExecutionController":          DiscriminatorExecutionController,
		"ClientSeat":                   DiscriminatorClientSeat,
		"PaymentEscrow":                DiscriminatorPaymentEscrow,
		"ShredDistribution":            DiscriminatorShredDistribution,
		"ValidatorClientRewards":       DiscriminatorValidatorClientRewards,
		"InstantSeatAllocationRequest": DiscriminatorInstantSeatAllocationRequest,
		"WithdrawSeatRequest":          DiscriminatorWithdrawSeatRequest,
		"MetroHistory":                 DiscriminatorMetroHistory,
		"DeviceHistory":                DiscriminatorDeviceHistory,
		"FeedDistribution":             DiscriminatorFeedDistribution,
	}

	seen := make(map[[8]byte]string)
	for name, disc := range discs {
		if prev, ok := seen[disc]; ok {
			t.Errorf("discriminator collision: %s and %s both produce %x", prev, name, disc)
		}
		seen[disc] = name
	}
}

func TestValidateDiscriminator(t *testing.T) {
	data := make([]byte, 16)
	copy(data[:8], DiscriminatorProgramConfig[:])

	if err := validateDiscriminator(data, DiscriminatorProgramConfig); err != nil {
		t.Fatalf("expected valid discriminator: %v", err)
	}

	if err := validateDiscriminator(data, DiscriminatorClientSeat); err == nil {
		t.Fatal("expected error for wrong discriminator")
	}

	if err := validateDiscriminator(data[:4], DiscriminatorProgramConfig); err == nil {
		t.Fatal("expected error for short data")
	}
}

// The seed string is hashed at runtime, so pin the result against the value the
// deployed program uses. A ::v3 bump upstream then fails here rather than
// quietly matching nothing on chain.
func TestDiscriminatorFeedDistributionMatchesOnchainSeed(t *testing.T) {
	const want = "38677e51559a48dc"
	if got := fmt.Sprintf("%x", DiscriminatorFeedDistribution); got != want {
		t.Errorf("DiscriminatorFeedDistribution = %s, want %s", got, want)
	}
}
