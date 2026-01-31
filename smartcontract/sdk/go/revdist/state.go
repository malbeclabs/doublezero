package revdist

import (
	"github.com/gagliardetto/solana-go"
)

// ProgramConfig represents the on-chain program configuration account.
// On-chain size: 8 (discriminator) + 600 = 608 bytes.
type ProgramConfig struct {
	Flags                               uint64           // 8 bytes
	NextCompletedDZEpoch                uint64           // 8 bytes
	BumpSeed                            uint8            // 1 byte
	Reserve2ZBumpSeed                   uint8            // 1 byte
	SwapAuthorityBumpSeed               uint8            // 1 byte
	SwapDestination2ZBumpSeed           uint8            // 1 byte
	WithdrawSOLAuthorityBumpSeed        uint8            // 1 byte
	Reserved0                           [3]uint8         // 3 bytes padding
	AdminKey                            solana.PublicKey  // 32 bytes
	DebtAccountantKey                   solana.PublicKey  // 32 bytes
	RewardsAccountantKey                solana.PublicKey  // 32 bytes
	ContributorManagerKey               solana.PublicKey  // 32 bytes
	PlaceholderKey                      solana.PublicKey  // 32 bytes
	SOL2ZSwapProgramID                  solana.PublicKey  // 32 bytes
	DistributionParameters              DistributionParameters
	RelayParameters                     RelayParameters
	LastInitializedDistributionTimestamp uint32           // 4 bytes
	Reserved1                           [4]byte          // 4 bytes padding
	DebtWriteOffFeatureActivationEpoch  uint64           // 8 bytes
}

// DistributionParameters contains epoch distribution configuration.
// 328 bytes total.
type DistributionParameters struct {
	CalculationGracePeriodMinutes         uint16 // 2 bytes
	InitializationGracePeriodMinutes      uint16 // 2 bytes
	MinimumEpochDurationToFinalizeRewards uint8  // 1 byte
	Reserved0                             [3]uint8
	CommunityBurnRateParameters           CommunityBurnRateParameters
	SolanaValidatorFeeParameters          SolanaValidatorFeeParameters
	Reserved1                             [8][32]byte // StorageGap<8> = 256 bytes
}

// CommunityBurnRateParameters configures the community burn rate schedule.
// 24 bytes total.
type CommunityBurnRateParameters struct {
	Limit                  uint32 // BurnRate (UnitShare32), max 1_000_000_000
	DZEpochsToIncreasing   uint32 // EpochDuration
	DZEpochsToLimit        uint32 // EpochDuration
	CachedSlopeNumerator   uint32 // BurnRate
	CachedSlopeDenominator uint32 // EpochDuration
	CachedNextBurnRate     uint32 // BurnRate
}

// SolanaValidatorFeeParameters configures validator fee percentages.
// 40 bytes total.
type SolanaValidatorFeeParameters struct {
	BaseBlockRewardsPct     uint16    // ValidatorFee (UnitShare16), max 10_000
	PriorityBlockRewardsPct uint16    // ValidatorFee
	InflationRewardsPct     uint16    // ValidatorFee
	JitoTipsPct             uint16    // ValidatorFee
	FixedSOLAmount          uint32    // 4 bytes
	Reserved0               [7]uint32 // 28 bytes storage gap
}

// RelayParameters configures relay lamport amounts.
// 40 bytes total.
type RelayParameters struct {
	PlaceholderLamports       uint32   // 4 bytes
	DistributeRewardsLamports uint32   // 4 bytes
	Reserved0                 [32]byte // 32 bytes storage gap
}

// Distribution represents a single epoch's distribution account.
// On-chain size: 8 (discriminator) + 448 = 456 bytes.
type Distribution struct {
	DZEpoch                                        uint64  // 8 bytes
	Flags                                          uint64  // 8 bytes
	CommunityBurnRate                              uint32  // 4 bytes (BurnRate)
	BumpSeed                                       uint8   // 1 byte
	Token2ZPDABumpSeed                             uint8   // 1 byte
	Reserved0                                      [2]byte // 2 bytes padding
	SolanaValidatorFeeParameters                   SolanaValidatorFeeParameters
	SolanaValidatorDebtMerkleRoot                  [32]byte // 32 bytes
	TotalSolanaValidators                          uint32   // 4 bytes
	SolanaValidatorPaymentsCount                   uint32   // 4 bytes
	TotalSolanaValidatorDebt                       uint64   // 8 bytes
	CollectedSolanaValidatorPayments               uint64   // 8 bytes
	RewardsMerkleRoot                              [32]byte // 32 bytes
	TotalContributors                              uint32   // 4 bytes
	DistributedRewardsCount                        uint32   // 4 bytes
	CollectedPrepaid2ZPayments                     uint64   // 8 bytes
	Collected2ZConvertedFromSOL                    uint64   // 8 bytes
	UncollectibleSOLDebt                           uint64   // 8 bytes
	ProcessedSolanaValidatorDebtStartIndex         uint32   // 4 bytes
	ProcessedSolanaValidatorDebtEndIndex           uint32   // 4 bytes
	ProcessedRewardsStartIndex                     uint32   // 4 bytes
	ProcessedRewardsEndIndex                       uint32   // 4 bytes
	DistributeRewardsRelayLamports                 uint32   // 4 bytes
	CalculationAllowedTimestamp                    uint32   // 4 bytes
	Distributed2ZAmount                            uint64   // 8 bytes
	Burned2ZAmount                                 uint64   // 8 bytes
	ProcessedSolanaValidatorDebtWriteOffStartIndex uint32   // 4 bytes
	ProcessedSolanaValidatorDebtWriteOffEndIndex   uint32   // 4 bytes
	SolanaValidatorWriteOffCount                   uint32   // 4 bytes
	Reserved1                                      [20]byte // 20 bytes padding
	Reserved2                                      [6][32]byte
}

// SolanaValidatorDeposit represents a validator's deposit account.
// On-chain size: 8 (discriminator) + 96 = 104 bytes.
type SolanaValidatorDeposit struct {
	NodeID            solana.PublicKey // 32 bytes
	WrittenOffSOLDebt uint64          // 8 bytes
	Reserved0         [24]byte        // 24 bytes padding
	Reserved1         [32]byte        // 32 bytes storage gap
}

// ContributorRewards represents a contributor's reward configuration.
// On-chain size: 8 (discriminator) + 600 = 608 bytes.
type ContributorRewards struct {
	RewardsManagerKey solana.PublicKey // 32 bytes
	ServiceKey        solana.PublicKey // 32 bytes
	Flags             uint64          // 8 bytes
	RecipientShares   RecipientShares // 272 bytes
	Reserved0         [8][32]byte     // 256 bytes storage gap
}

// RecipientShare represents a single reward recipient and their share.
// 34 bytes total.
type RecipientShare struct {
	RecipientKey solana.PublicKey // 32 bytes
	Share        uint16          // UnitShare16, max 10_000 (100%)
}

// RecipientShares is a fixed array of 8 RecipientShare entries.
// 272 bytes total (8 * 34).
type RecipientShares [8]RecipientShare

// Journal tracks aggregate balances across the program.
// On-chain size: 8 (discriminator) + 64 = 72 bytes.
type Journal struct {
	BumpSeed                 uint8    // 1 byte
	Token2ZPDABumpSeed       uint8    // 1 byte
	Reserved0                [6]byte  // 6 bytes padding
	TotalSOLBalance          uint64   // 8 bytes
	Total2ZBalance           uint64   // 8 bytes
	Swap2ZDestinationBalance uint64   // 8 bytes
	SwappedSOLAmount         uint64   // 8 bytes
	NextDZEpochToSweepTokens uint64   // 8 bytes
	LifetimeSwapped2ZAmount  [16]byte // 16 bytes (u128 LE)
}

// ComputedSolanaValidatorDebts is a Borsh-serialized off-chain record
// containing validator debt calculations for an epoch range.
type ComputedSolanaValidatorDebts struct {
	Blockhash        [32]byte                      // 32 bytes
	FirstSolanaEpoch uint64                        // 8 bytes
	LastSolanaEpoch  uint64                        // 8 bytes
	Debts            []ComputedSolanaValidatorDebt // Borsh Vec
}

// ComputedSolanaValidatorDebt represents a single validator's calculated debt.
type ComputedSolanaValidatorDebt struct {
	NodeID solana.PublicKey // 32 bytes
	Amount uint64          // 8 bytes
}

// ShapleyOutputStorage is a Borsh-serialized off-chain record
// containing Shapley value reward calculations.
type ShapleyOutputStorage struct {
	Epoch           uint64        // 8 bytes
	Rewards         []RewardShare // Borsh Vec
	TotalUnitShares uint32        // 4 bytes
}

// RewardShare represents a contributor's calculated reward share.
type RewardShare struct {
	ContributorKey solana.PublicKey // 32 bytes
	UnitShare      uint32          // 4 bytes (UnitShare32)
	RemainingBytes [4]byte         // 4 bytes (bit 31 = is_blocked, bits 0-29 = economic_burn_rate)
}
