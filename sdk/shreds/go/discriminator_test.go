package shreds

import (
	"fmt"
	"testing"
)

func TestValidateDiscriminator(t *testing.T) {
	data := make([]byte, 16)
	copy(data[:8], DiscriminatorFeedDistribution[:])

	if err := validateDiscriminator(data, DiscriminatorFeedDistribution); err != nil {
		t.Fatalf("expected valid discriminator: %v", err)
	}

	if err := validateDiscriminator(data, sha256First8("dz::account::feed_distribution")); err == nil {
		t.Fatal("expected error for wrong discriminator")
	}

	if err := validateDiscriminator(data[:4], DiscriminatorFeedDistribution); err == nil {
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
