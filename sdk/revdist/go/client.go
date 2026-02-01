package revdist

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	ag_binary "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

var (
	ErrAccountNotFound = errors.New("account not found")
	ErrLedgerClientNil = errors.New("ledger record client not configured")
)

// deserializeAccount validates the discriminator and deserializes the account
// data into the given struct. It requires at least discriminator + sizeof(T)
// bytes but tolerates extra trailing bytes for forward compatibility.
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
	GetProgramAccounts(ctx context.Context, publicKey solana.PublicKey) (rpc.GetProgramAccountsResult, error)
	GetProgramAccountsWithOpts(ctx context.Context, publicKey solana.PublicKey, opts *rpc.GetProgramAccountsOpts) (rpc.GetProgramAccountsResult, error)
	GetMinimumBalanceForRentExemption(ctx context.Context, dataSize uint64, commitment rpc.CommitmentType) (uint64, error)
}

// LedgerRecordClient fetches off-chain record data from the DZ Ledger.
type LedgerRecordClient interface {
	GetRecordData(ctx context.Context, account solana.PublicKey) ([]byte, error)
}

// Client provides read-only access to revenue distribution program accounts.
type Client struct {
	rpc          RPCClient
	programID    solana.PublicKey
	ledgerClient LedgerRecordClient
}

// New creates a new revenue distribution client.
func New(rpc RPCClient, programID solana.PublicKey) *Client {
	return &Client{
		rpc:       rpc,
		programID: programID,
	}
}

// NewWithLedger creates a new client with ledger record support.
func NewWithLedger(rpc RPCClient, programID solana.PublicKey, ledgerClient LedgerRecordClient) *Client {
	return &Client{
		rpc:          rpc,
		programID:    programID,
		ledgerClient: ledgerClient,
	}
}

func (c *Client) FetchConfig(ctx context.Context) (*ProgramConfig, error) {
	addr, _, err := DeriveConfigPDA(c.programID)
	if err != nil {
		return nil, fmt.Errorf("deriving config PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[ProgramConfig](data, DiscriminatorProgramConfig)
}

func (c *Client) FetchDistribution(ctx context.Context, epoch uint64) (*Distribution, error) {
	addr, _, err := DeriveDistributionPDA(c.programID, epoch)
	if err != nil {
		return nil, fmt.Errorf("deriving distribution PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[Distribution](data, DiscriminatorDistribution)
}

func (c *Client) FetchJournal(ctx context.Context) (*Journal, error) {
	addr, _, err := DeriveJournalPDA(c.programID)
	if err != nil {
		return nil, fmt.Errorf("deriving journal PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[Journal](data, DiscriminatorJournal)
}

func (c *Client) FetchValidatorDeposit(ctx context.Context, nodeID solana.PublicKey) (*SolanaValidatorDeposit, error) {
	addr, _, err := DeriveValidatorDepositPDA(c.programID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("deriving validator deposit PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[SolanaValidatorDeposit](data, DiscriminatorSolanaValidatorDeposit)
}

func (c *Client) FetchAllValidatorDeposits(ctx context.Context) ([]SolanaValidatorDeposit, error) {
	return fetchAllByDiscriminator[SolanaValidatorDeposit](ctx, c, DiscriminatorSolanaValidatorDeposit)
}

func (c *Client) FetchContributorRewards(ctx context.Context, serviceKey solana.PublicKey) (*ContributorRewards, error) {
	addr, _, err := DeriveContributorRewardsPDA(c.programID, serviceKey)
	if err != nil {
		return nil, fmt.Errorf("deriving contributor rewards PDA: %w", err)
	}
	data, err := c.fetchAccountData(ctx, addr)
	if err != nil {
		return nil, err
	}
	return deserializeAccount[ContributorRewards](data, DiscriminatorContributorRewards)
}

func (c *Client) FetchAllContributorRewards(ctx context.Context) ([]ContributorRewards, error) {
	return fetchAllByDiscriminator[ContributorRewards](ctx, c, DiscriminatorContributorRewards)
}

// ValidatorDepositBalance returns the effective deposit balance for a validator,
// computed as account_lamports - rent_exempt_minimum.
func (c *Client) ValidatorDepositBalance(ctx context.Context, nodeID solana.PublicKey) (uint64, error) {
	addr, _, err := DeriveValidatorDepositPDA(c.programID, nodeID)
	if err != nil {
		return 0, fmt.Errorf("deriving validator deposit PDA: %w", err)
	}
	result, err := c.rpc.GetAccountInfo(ctx, addr)
	if err != nil {
		return 0, fmt.Errorf("fetching account: %w", err)
	}
	if result == nil || result.Value == nil {
		return 0, ErrAccountNotFound
	}
	lamports := result.Value.Lamports
	rentExempt, err := c.rpc.GetMinimumBalanceForRentExemption(ctx, uint64(len(result.Value.Data.GetBinary())), rpc.CommitmentFinalized)
	if err != nil {
		return 0, fmt.Errorf("fetching rent exemption: %w", err)
	}
	if lamports <= rentExempt {
		return 0, nil
	}
	return lamports - rentExempt, nil
}

// FetchValidatorDebts fetches and deserializes the off-chain validator debt
// record for the given DZ epoch from the DZ Ledger.
func (c *Client) FetchValidatorDebts(ctx context.Context, epoch uint64) (*ComputedSolanaValidatorDebts, error) {
	if c.ledgerClient == nil {
		return nil, ErrLedgerClientNil
	}
	config, err := c.FetchConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching config for debt accountant key: %w", err)
	}
	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, epoch)
	addr, err := DeriveRecordKey(config.DebtAccountantKey, [][]byte{
		seedSolanaValidatorDebt,
		epochBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("deriving validator debt record key: %w", err)
	}
	data, err := c.ledgerClient.GetRecordData(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("fetching validator debt record: %w", err)
	}
	if len(data) <= recordHeaderSize {
		return nil, fmt.Errorf("validator debt record data too short: %d bytes", len(data))
	}
	var debts ComputedSolanaValidatorDebts
	decoder := ag_binary.NewBorshDecoder(data[recordHeaderSize:])
	if err := decoder.Decode(&debts); err != nil {
		return nil, fmt.Errorf("deserializing validator debts: %w", err)
	}
	return &debts, nil
}

// FetchRewardShares fetches and deserializes the off-chain Shapley output
// record for the given DZ epoch from the DZ Ledger.
func (c *Client) FetchRewardShares(ctx context.Context, epoch uint64) (*ShapleyOutputStorage, error) {
	if c.ledgerClient == nil {
		return nil, ErrLedgerClientNil
	}
	config, err := c.FetchConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching config for rewards accountant key: %w", err)
	}
	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, epoch)
	addr, err := DeriveRecordKey(config.RewardsAccountantKey, [][]byte{
		seedDZContributorRewards,
		epochBytes,
		seedShapleyOutput,
	})
	if err != nil {
		return nil, fmt.Errorf("deriving reward shares record key: %w", err)
	}
	data, err := c.ledgerClient.GetRecordData(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("fetching reward shares record: %w", err)
	}
	if len(data) <= recordHeaderSize {
		return nil, fmt.Errorf("reward shares record data too short: %d bytes", len(data))
	}
	var output ShapleyOutputStorage
	decoder := ag_binary.NewBorshDecoder(data[recordHeaderSize:])
	if err := decoder.Decode(&output); err != nil {
		return nil, fmt.Errorf("deserializing reward shares: %w", err)
	}
	return &output, nil
}

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

func fetchAllByDiscriminator[T any](ctx context.Context, c *Client, disc [8]byte) ([]T, error) {
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
	results := make([]T, 0, len(accounts))
	for _, acct := range accounts {
		data := acct.Account.Data.GetBinary()
		item, err := deserializeAccount[T](data, disc)
		if err != nil {
			return nil, fmt.Errorf("deserializing account %s: %w", acct.Pubkey, err)
		}
		results = append(results, *item)
	}
	return results, nil
}
