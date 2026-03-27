package shreds

import "github.com/gagliardetto/solana-go"

// MaxHistoryCount is the number of epoch entries in each ring buffer.
const MaxHistoryCount = 32

// MaxValidatorClientRewardProportions is the capacity of the proportions array.
const MaxValidatorClientRewardProportions = 32

// ExecutionPhase represents the current phase of the epoch state machine.
type ExecutionPhase uint8

const (
	ExecutionPhaseClosedForRequests ExecutionPhase = 0
	ExecutionPhaseUpdatingPrices    ExecutionPhase = 1
	ExecutionPhaseOpenForRequests   ExecutionPhase = 2
)

func (p ExecutionPhase) String() string {
	switch p {
	case ExecutionPhaseClosedForRequests:
		return "closed for requests"
	case ExecutionPhaseUpdatingPrices:
		return "updating prices"
	case ExecutionPhaseOpenForRequests:
		return "open for requests"
	default:
		return "unknown"
	}
}

// ValidatorClientRewardsProportion pairs a validator client ID with its
// reward share (basis points, max 10 000).
type ValidatorClientRewardsProportion struct {
	ID               uint16 // Validator client ID
	RewardProportion uint16 // UnitShare16
}

// ValidatorClientRewardProportionsArray is the fixed-size array of reward
// proportions stored inside ProgramConfig and ShredDistribution.
type ValidatorClientRewardProportionsArray [MaxValidatorClientRewardProportions]ValidatorClientRewardsProportion

// ValidatorClientRewardsConfig configures how rewards are split across
// validator clients.
type ValidatorClientRewardsConfig struct {
	DefaultProportion uint16  // UnitShare16
	Padding0          [6]byte // alignment
	Proportions       ValidatorClientRewardProportionsArray
}

// ProgramConfig is the global program configuration account.
type ProgramConfig struct {
	Flags                             uint64           // Flags
	AdminKey                          solana.PublicKey // 32 bytes
	ClosedForRequestsGracePeriodSlots uint32
	USDC2ZMaxSlippageBps              uint16 // UnitShare16
	USDC2ZConversionGracePeriodEpochs uint8
	Padding0                          [1]byte
	ShredOracleKey                    solana.PublicKey
	USDC2ZOracleKey                   solana.PublicKey
	ValidatorClientRewardsConfig      ValidatorClientRewardsConfig
}

// ExecutionController tracks the epoch state machine and settlement progress.
type ExecutionController struct {
	Phase                     uint8 // ExecutionPhase
	BumpSeed                  uint8
	Padding0                  [2]byte
	TotalMetros               uint16
	TotalEnabledDevices       uint16
	TotalClientSeats          uint32
	OracleInstantRequestCount uint16
	ValidatorClientIDsCount   uint8
	Padding1                  [1]byte
	Flags                     uint64 // Flags
	CurrentSubscriptionEpoch  uint64
	UpdatedDevicePricesCount  uint16
	SettledDevicesCount       uint16
	SettledClientSeatsCount   uint16
	Padding2                  [2]byte
	LastSettledSlot           uint64
	LastUpdatingPricesSlot    uint64
	LastOpenForRequestsSlot   uint64
	LastClosedForRequestsSlot uint64
	EpochRoundCommitment      [32]byte
	EpochRoundReveal          [32]byte
	NextSeatFundingIndex      uint64
}

// GetPhase returns the execution phase as a typed enum.
func (e *ExecutionController) GetPhase() ExecutionPhase {
	return ExecutionPhase(e.Phase)
}

// ClientSeat represents one client's subscription seat on a device.
type ClientSeat struct {
	DeviceKey                solana.PublicKey
	ClientIPBits             uint32
	Padding0                 [2]byte
	TenureEpochs             uint16
	Flags                    uint64
	FundedEpoch              uint64
	ActiveEpoch              uint64
	NewFundingIndex          uint64
	NewSettlementSortKey     [32]byte
	FundingAuthorityKey      solana.PublicKey
	EscrowCount              uint32
	OverrideUSDCPriceDollars uint16
	Padding1                 [26]byte
	Gap                      [2][32]byte // StorageGap<2>
}

// HasPriceOverride returns true if a flat price override is active.
func (s *ClientSeat) HasPriceOverride() bool {
	return s.Flags&1 != 0
}

// PaymentEscrow holds USDC balance funding a client seat.
type PaymentEscrow struct {
	ClientSeatKey        solana.PublicKey
	WithdrawAuthorityKey solana.PublicKey
	USDCBalance          uint64
	Gap                  [2][32]byte // StorageGap<2>
}

// ShredDistribution tracks payment collection and reward distribution for a
// single subscription epoch.
type ShredDistribution struct {
	SubscriptionEpoch                     uint64
	Flags                                 uint64
	AssociatedDZEpoch                     uint64
	BumpSeed                              uint8
	ATAUSDBumpSeed                        uint8
	ATA2ZBumpSeed                         uint8
	Padding0                              [1]byte
	DeviceCount                           uint16
	ClientSeatCount                       uint16
	Padding1                              [2]byte
	ValidatorRewardsProportion            uint16 // UnitShare16
	TotalPublishingValidators             uint32
	ValidatorRewardsMerkleRoot            [32]byte
	CollectedUSDCPayments                 uint64
	Collected2ZConvertedFromUSDC          uint64
	DistributedValidatorRewardsCount      uint32
	DistributedContributorRewardsCount    uint32
	DistributedValidator2ZAmount          uint64
	DistributedContributor2ZAmount        uint64
	Burned2ZAmount                        uint64
	ProcessedValidatorRewardsStartIndex   uint32
	ProcessedValidatorRewardsEndIndex     uint32
	ProcessedContributorRewardsStartIndex uint32
	ProcessedContributorRewardsEndIndex   uint32
	ValidatorClientRewardProportions      ValidatorClientRewardProportionsArray
	Gap                                   [4][32]byte // StorageGap<4>
}

// ValidatorClientRewards is a registered validator client eligible for reward
// distribution.
type ValidatorClientRewards struct {
	ClientID              uint16
	Padding0              [6]byte
	ManagerKey            solana.PublicKey
	ShortDescriptionBytes [64]byte
	Gap                   [2][32]byte // StorageGap<2>
}

// ShortDescription returns the UTF-8 description with trailing nulls trimmed.
func (v *ValidatorClientRewards) ShortDescription() string {
	end := len(v.ShortDescriptionBytes)
	for end > 0 && v.ShortDescriptionBytes[end-1] == 0 {
		end--
	}
	return string(v.ShortDescriptionBytes[:end])
}

// InstantSeatAllocationRequest is an ephemeral account requesting immediate
// seat allocation.
type InstantSeatAllocationRequest struct {
	DeviceKey          solana.PublicKey
	ClientIPBits       uint32
	Padding0           [4]byte
	RequiredUSDCAmount uint64
}

// WithdrawSeatRequest is an ephemeral account requesting seat withdrawal.
type WithdrawSeatRequest struct {
	DeviceKey    solana.PublicKey
	ClientIPBits uint32
}

// --- History types ---

// MetroPrice is the per-epoch metro price.
type MetroPrice struct {
	USDCPriceDollars uint16
	Padding0         [6]byte
	Gap              [2][32]byte // StorageGap<2>
}

// MetroPriceEntry is an epoch-stamped metro price.
type MetroPriceEntry struct {
	Epoch uint64
	Price MetroPrice
}

// MetroPriceRingBuffer stores the last 32 epochs of metro prices.
type MetroPriceRingBuffer struct {
	CurrentIndex uint8
	TotalCount   uint8
	Padding0     [6]byte
	Entries      [MaxHistoryCount]MetroPriceEntry
}

// MetroHistory tracks pricing history for a metro area.
type MetroHistory struct {
	ExchangeKey             solana.PublicKey
	Flags                   uint64
	TotalInitializedDevices uint16
	Padding0                [6]byte
	Gap                     [4][32]byte // StorageGap<4>
	Prices                  MetroPriceRingBuffer
}

// IsCurrentPriceFinalized returns true if the current epoch price is finalized.
func (m *MetroHistory) IsCurrentPriceFinalized() bool {
	return m.Flags&(1<<1) != 0
}

// DeviceSubscription is the per-epoch device subscription data.
type DeviceSubscription struct {
	USDCMetroPremiumDollars int16
	RequestedSeatCount      uint16
	TotalAvailableSeats     uint16
	GrantedSeatCount        uint16
	Gap                     [2][32]byte // StorageGap<2>
}

// DeviceSubscriptionEntry is an epoch-stamped device subscription.
type DeviceSubscriptionEntry struct {
	Epoch        uint64
	Subscription DeviceSubscription
}

// DeviceSubscriptionRingBuffer stores the last 32 epochs of device
// subscriptions.
type DeviceSubscriptionRingBuffer struct {
	CurrentIndex uint8
	TotalCount   uint8
	Padding0     [6]byte
	Entries      [MaxHistoryCount]DeviceSubscriptionEntry
}

// DeviceHistory tracks subscription history for a device.
type DeviceHistory struct {
	DeviceKey                 solana.PublicKey
	Flags                     uint64
	BumpSeed                  uint8
	USDCTokenPDABumpSeed      uint8
	Padding0                  [6]byte
	MetroExchangeKey          solana.PublicKey
	ActiveGrantedSeats        uint16
	ActiveTotalAvailableSeats uint16
	ActiveSeatsPadding        [28]byte
	Gap                       [3][32]byte // StorageGap<3>
	Subscriptions             DeviceSubscriptionRingBuffer
}

// IsEnabled returns true if this device is enabled for subscriptions.
func (d *DeviceHistory) IsEnabled() bool {
	return d.Flags&(1<<1) != 0
}

// HasSettledSeats returns true if this device has settled seats in the current epoch.
func (d *DeviceHistory) HasSettledSeats() bool {
	return d.Flags&(1<<2) != 0
}

// --- Keyed wrappers for batch fetches ---

// KeyedClientSeat pairs a client seat with its onchain address.
type KeyedClientSeat struct {
	Pubkey solana.PublicKey
	ClientSeat
}

// KeyedPaymentEscrow pairs a payment escrow with its onchain address.
type KeyedPaymentEscrow struct {
	Pubkey solana.PublicKey
	PaymentEscrow
}

// KeyedMetroHistory pairs a metro history with its onchain address.
type KeyedMetroHistory struct {
	Pubkey solana.PublicKey
	MetroHistory
}

// KeyedDeviceHistory pairs a device history with its onchain address.
type KeyedDeviceHistory struct {
	Pubkey solana.PublicKey
	DeviceHistory
}

// KeyedValidatorClientRewards pairs a validator client rewards account with its
// onchain address.
type KeyedValidatorClientRewards struct {
	Pubkey solana.PublicKey
	ValidatorClientRewards
}
