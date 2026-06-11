package shreds

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

var ErrAccountNotFound = errors.New("account not found")

// deserializeAccount validates the discriminator and deserializes the account
// data into the given struct type. Tolerates trailing bytes for forward
// compatibility.
func deserializeAccount[T any](data []byte, disc [8]byte) (*T, error) {
	if err := validateDiscriminator(data, disc); err != nil {
		return nil, err
	}
	body := data[discriminatorSize:]
	var zero T
	need := int(unsafe.Sizeof(zero))
	if len(body) < need {
		return nil, fmt.Errorf("account data too short: have %d bytes, need at least %d", len(body), need)
	}
	var item T
	if err := binary.Read(bytes.NewReader(body[:need]), binary.LittleEndian, &item); err != nil {
		return nil, fmt.Errorf("deserializing account: %w", err)
	}
	return &item, nil
}

// RPCClient is the minimal RPC interface needed by the client.
type RPCClient interface {
	GetAccountInfo(ctx context.Context, account solana.PublicKey) (*rpc.GetAccountInfoResult, error)
	GetProgramAccountsWithOpts(ctx context.Context, publicKey solana.PublicKey, opts *rpc.GetProgramAccountsOpts) (rpc.GetProgramAccountsResult, error)
}

// Client provides read-only access to shred subscription program accounts.
type Client struct {
	rpc       RPCClient
	programID solana.PublicKey
}

// New creates a new shred subscription client.
func New(rpc RPCClient, programID solana.PublicKey) *Client {
	return &Client{rpc: rpc, programID: programID}
}

// NewForEnv creates a client configured for the given environment.
func NewForEnv(env string) *Client {
	return New(NewRPCClient(SolanaRPCURLs[env]), ProgramID)
}

// NewMainnetBeta creates a client configured for mainnet-beta.
func NewMainnetBeta() *Client { return NewForEnv("mainnet-beta") }

// NewTestnet creates a client configured for testnet.
func NewTestnet() *Client { return NewForEnv("testnet") }

// NewDevnet creates a client configured for devnet.
func NewDevnet() *Client { return NewForEnv("devnet") }

// NewLocalnet creates a client configured for localnet.
func NewLocalnet() *Client { return NewForEnv("localnet") }

// ProgramID returns the configured program ID.
func (c *Client) ProgramID() solana.PublicKey { return c.programID }

// --- Singleton fetches ---

func (c *Client) FetchProgramConfig(ctx context.Context) (*ProgramConfig, error) {
	addr, _, err := DeriveProgramConfigPDA(c.programID)
	if err != nil {
		return nil, fmt.Errorf("deriving program config PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[ProgramConfig](data, DiscriminatorProgramConfig)
}

func (c *Client) FetchExecutionController(ctx context.Context) (*ExecutionController, error) {
	addr, _, err := DeriveExecutionControllerPDA(c.programID)
	if err != nil {
		return nil, fmt.Errorf("deriving execution controller PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[ExecutionController](data, DiscriminatorExecutionController)
}

// --- Keyed fetches ---

func (c *Client) FetchClientSeat(ctx context.Context, deviceKey solana.PublicKey, clientIPBits uint32) (*ClientSeat, error) {
	addr, _, err := DeriveClientSeatPDA(c.programID, deviceKey, clientIPBits)
	if err != nil {
		return nil, fmt.Errorf("deriving client seat PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[ClientSeat](data, DiscriminatorClientSeat)
}

func (c *Client) FetchPaymentEscrow(ctx context.Context, clientSeatKey, withdrawAuthorityKey solana.PublicKey) (*PaymentEscrow, error) {
	addr, _, err := DerivePaymentEscrowPDA(c.programID, clientSeatKey, withdrawAuthorityKey)
	if err != nil {
		return nil, fmt.Errorf("deriving payment escrow PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[PaymentEscrow](data, DiscriminatorPaymentEscrow)
}

func (c *Client) FetchShredDistribution(ctx context.Context, subscriptionEpoch uint64) (*ShredDistribution, error) {
	addr, _, err := DeriveShredDistributionPDA(c.programID, subscriptionEpoch)
	if err != nil {
		return nil, fmt.Errorf("deriving shred distribution PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[ShredDistribution](data, DiscriminatorShredDistribution)
}

func (c *Client) FetchValidatorClientRewards(ctx context.Context, clientID uint16) (*ValidatorClientRewards, error) {
	addr, _, err := DeriveValidatorClientRewardsPDA(c.programID, clientID)
	if err != nil {
		return nil, fmt.Errorf("deriving validator client rewards PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[ValidatorClientRewards](data, DiscriminatorValidatorClientRewards)
}

func (c *Client) FetchMetroHistory(ctx context.Context, exchangeKey solana.PublicKey) (*MetroHistory, error) {
	addr, _, err := DeriveMetroHistoryPDA(c.programID, exchangeKey)
	if err != nil {
		return nil, fmt.Errorf("deriving metro history PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[MetroHistory](data, DiscriminatorMetroHistory)
}

func (c *Client) FetchDeviceHistory(ctx context.Context, deviceKey solana.PublicKey) (*DeviceHistory, error) {
	addr, _, err := DeriveDeviceHistoryPDA(c.programID, deviceKey)
	if err != nil {
		return nil, fmt.Errorf("deriving device history PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[DeviceHistory](data, DiscriminatorDeviceHistory)
}

func (c *Client) FetchInstantSeatAllocationRequest(ctx context.Context, deviceKey solana.PublicKey, clientIPBits uint32) (*InstantSeatAllocationRequest, error) {
	addr, _, err := DeriveInstantSeatAllocationRequestPDA(c.programID, deviceKey, clientIPBits)
	if err != nil {
		return nil, fmt.Errorf("deriving instant seat allocation request PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[InstantSeatAllocationRequest](data, DiscriminatorInstantSeatAllocationRequest)
}

func (c *Client) FetchWithdrawSeatRequest(ctx context.Context, clientSeatKey solana.PublicKey) (*WithdrawSeatRequest, error) {
	addr, _, err := DeriveWithdrawSeatRequestPDA(c.programID, clientSeatKey)
	if err != nil {
		return nil, fmt.Errorf("deriving withdraw seat request PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[WithdrawSeatRequest](data, DiscriminatorWithdrawSeatRequest)
}

// --- Batch fetches ---

func (c *Client) FetchAllClientSeats(ctx context.Context) ([]KeyedClientSeat, error) {
	return fetchAllKeyed[ClientSeat, KeyedClientSeat](ctx, c, DiscriminatorClientSeat, func(pk solana.PublicKey, v ClientSeat) KeyedClientSeat {
		return KeyedClientSeat{Pubkey: pk, ClientSeat: v}
	})
}

func (c *Client) FetchAllPaymentEscrows(ctx context.Context) ([]KeyedPaymentEscrow, error) {
	return fetchAllKeyed[PaymentEscrow, KeyedPaymentEscrow](ctx, c, DiscriminatorPaymentEscrow, func(pk solana.PublicKey, v PaymentEscrow) KeyedPaymentEscrow {
		return KeyedPaymentEscrow{Pubkey: pk, PaymentEscrow: v}
	})
}

func (c *Client) FetchAllMetroHistories(ctx context.Context) ([]KeyedMetroHistory, error) {
	return fetchAllKeyed[MetroHistory, KeyedMetroHistory](ctx, c, DiscriminatorMetroHistory, func(pk solana.PublicKey, v MetroHistory) KeyedMetroHistory {
		return KeyedMetroHistory{Pubkey: pk, MetroHistory: v}
	})
}

func (c *Client) FetchAllDeviceHistories(ctx context.Context) ([]KeyedDeviceHistory, error) {
	return fetchAllKeyed[DeviceHistory, KeyedDeviceHistory](ctx, c, DiscriminatorDeviceHistory, func(pk solana.PublicKey, v DeviceHistory) KeyedDeviceHistory {
		return KeyedDeviceHistory{Pubkey: pk, DeviceHistory: v}
	})
}

func (c *Client) FetchAllValidatorClientRewards(ctx context.Context) ([]KeyedValidatorClientRewards, error) {
	return fetchAllKeyed[ValidatorClientRewards, KeyedValidatorClientRewards](ctx, c, DiscriminatorValidatorClientRewards, func(pk solana.PublicKey, v ValidatorClientRewards) KeyedValidatorClientRewards {
		return KeyedValidatorClientRewards{Pubkey: pk, ValidatorClientRewards: v}
	})
}

// --- Internal helpers ---

func (c *Client) fetchAccountData(ctx context.Context, addr solana.PublicKey) ([]byte, error) {
	result, err := c.rpc.GetAccountInfo(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("fetching account %s: %w", addr, err)
	}
	if result == nil || result.Value == nil {
		return nil, ErrAccountNotFound
	}
	return result.Value.Data.GetBinary(), nil
}

func fetchAllKeyed[T any, K any](ctx context.Context, c *Client, disc [8]byte, wrap func(solana.PublicKey, T) K) ([]K, error) {
	opts := &rpc.GetProgramAccountsOpts{
		Filters: []rpc.RPCFilter{
			{
				Memcmp: &rpc.RPCFilterMemcmp{
					Offset: 0,
					Bytes:  disc[:],
				},
			},
		},
	}
	accounts, err := c.rpc.GetProgramAccountsWithOpts(ctx, c.programID, opts)
	if err != nil {
		return nil, fmt.Errorf("fetching program accounts: %w", err)
	}
	results := make([]K, 0, len(accounts))
	for _, acct := range accounts {
		data := acct.Account.Data.GetBinary()
		item, err := deserializeAccount[T](data, disc)
		if err != nil {
			return nil, fmt.Errorf("deserializing account %s: %w", acct.Pubkey, err)
		}
		results = append(results, wrap(acct.Pubkey, *item))
	}
	return results, nil
}
