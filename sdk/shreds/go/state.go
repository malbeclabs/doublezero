package shreds

import "github.com/gagliardetto/solana-go"

// FeedDistribution is how much USDC one feed collected for one calendar month.
// It belongs to the feed subscription program (FeedProgramID), which owns one
// such account per feed per month.
//
// CollectedUSDCAmount is USDC base units and only ever increases. Read a month
// as an allocation, not as cash that arrived in it: one subscription payment is
// credited across the calendar months the subscription spans, in day fractions,
// so an account can exist for a month that has not started. Sum every month for
// cash collected to date.
//
// The feed's vault balance is a different number, and it drains to zero once the
// month settles.
type FeedDistribution struct {
	FeedKey                       solana.PublicKey
	Year                          uint16
	Month                         uint8
	BumpSeed                      uint8
	PublisherRewardsProportionBps uint16
	PaymentAuthorityBumpSeed      uint8
	VaultUSDCATABumpSeed          uint8
	CollectedUSDCAmount           uint64
	Flags                         uint64      // Flags
	Gap                           [2][32]byte // StorageGap<2>
}
