package shreds

import (
	"encoding/base64"
	"testing"
	"unsafe"
)

func TestStructSizes(t *testing.T) {
	tests := []struct {
		name     string
		size     uintptr
		expected uintptr
	}{
		{"FeedDistribution", unsafe.Sizeof(FeedDistribution{}), 120},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.size != tt.expected {
				t.Errorf("sizeof(%s) = %d, want %d", tt.name, tt.size, tt.expected)
			}
		})
	}
}

// mainnetFeedDistribution is the full 128 bytes of feed distribution account
// crW8HCYDpQVyCxYG7m3hXeC42rAnjoLroGGfgGLLXM2 on Solana mainnet-beta, read on
// 2026-08-19: an 8-byte discriminator followed by the 120-byte struct.
//
// Real bytes, not synthesised ones. A fixture this package encoded itself would
// agree with any field-order mistake it also made.
const mainnetFeedDistribution = "OGd+UVWaSNwwUMWSdfRNaNAxqeB5i3T95mrqWd7yZShRKdBh7irZu+oHCP8AAP3/JyAEfAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

// TestFeedDistributionDeserialization is the layout guard for the one account
// this package carries from the feed subscription program.
//
// FeedDistribution is a bytemuck Pod onchain (#[repr(C, align(8))]). This
// package reads it field by field, which agrees with the Pod bytes only because
// the field order leaves no interior padding. TestStructSizes pins that total;
// this pins every field against known values, so a reordered or resized field
// fails here instead of reporting a plausible wrong amount.
//
// The three bump seeds around a zero proportion (255, 0, 253, 255) are what make
// a one-byte shift detectable rather than merely plausible.
func TestFeedDistributionDeserialization(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(mainnetFeedDistribution)
	if err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	if want := discriminatorSize + int(unsafe.Sizeof(FeedDistribution{})); len(data) != want {
		t.Fatalf("fixture is %d bytes, want %d", len(data), want)
	}

	dist, err := DeserializeFeedDistribution(data)
	if err != nil {
		t.Fatalf("DeserializeFeedDistribution: %v", err)
	}

	const wantFeedKey = "4Fc1Fyd1x8BoWYPWN8vFhbP6fpgayybQuLUSPRwfE7Wi"
	if got := dist.FeedKey.String(); got != wantFeedKey {
		t.Errorf("FeedKey = %s, want %s", got, wantFeedKey)
	}
	if dist.Year != 2026 {
		t.Errorf("Year = %d, want 2026", dist.Year)
	}
	if dist.Month != 8 {
		t.Errorf("Month = %d, want 8", dist.Month)
	}
	if dist.BumpSeed != 255 {
		t.Errorf("BumpSeed = %d, want 255", dist.BumpSeed)
	}
	if dist.PublisherRewardsProportionBps != 0 {
		t.Errorf("PublisherRewardsProportionBps = %d, want 0", dist.PublisherRewardsProportionBps)
	}
	if dist.PaymentAuthorityBumpSeed != 253 {
		t.Errorf("PaymentAuthorityBumpSeed = %d, want 253", dist.PaymentAuthorityBumpSeed)
	}
	if dist.VaultUSDCATABumpSeed != 255 {
		t.Errorf("VaultUSDCATABumpSeed = %d, want 255", dist.VaultUSDCATABumpSeed)
	}
	if dist.CollectedUSDCAmount != 2080645159 {
		t.Errorf("CollectedUSDCAmount = %d, want 2080645159", dist.CollectedUSDCAmount)
	}
}

// A v1 account carries a different discriminator and is orphaned: v2 replaced
// the per-month vault with a per-feed vault. Reading a v1 account as v2 would
// report a wrong collected amount, so the mismatch has to be an error.
func TestFeedDistributionRejectsWrongDiscriminator(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(mainnetFeedDistribution)
	if err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	data[0] ^= 0xff

	if _, err := DeserializeFeedDistribution(data); err == nil {
		t.Fatal("expected an error for a mismatched discriminator, got nil")
	}
}
