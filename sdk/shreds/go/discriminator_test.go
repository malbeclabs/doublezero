package shreds

import (
	"testing"
)

func TestDiscriminatorsAreUnique(t *testing.T) {
	discs := map[string][8]byte{
		"ProgramConfig":              DiscriminatorProgramConfig,
		"ExecutionController":        DiscriminatorExecutionController,
		"ClientSeat":                 DiscriminatorClientSeat,
		"PaymentEscrow":              DiscriminatorPaymentEscrow,
		"ShredDistribution":          DiscriminatorShredDistribution,
		"ValidatorClientRewards":     DiscriminatorValidatorClientRewards,
		"InstantSeatAllocationRequest": DiscriminatorInstantSeatAllocationRequest,
		"WithdrawSeatRequest":        DiscriminatorWithdrawSeatRequest,
		"MetroHistory":               DiscriminatorMetroHistory,
		"DeviceHistory":              DiscriminatorDeviceHistory,
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
