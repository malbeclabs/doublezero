package geolocation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

var (
	ErrAccountNotFound = errors.New("account not found")
)

type Client struct {
	log       *slog.Logger
	rpc       RPCClient
	programID solana.PublicKey
}

func New(log *slog.Logger, rpc RPCClient, programID solana.PublicKey) *Client {
	return &Client{
		log:       log,
		rpc:       rpc,
		programID: programID,
	}
}

func (c *Client) ProgramID() solana.PublicKey {
	return c.programID
}

// GetProgramConfig fetches the GeolocationProgramConfig account.
func (c *Client) GetProgramConfig(ctx context.Context) (*GeolocationProgramConfig, error) {
	pda, _, err := DeriveProgramConfigPDA(c.programID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive PDA: %w", err)
	}

	account, err := c.rpc.GetAccountInfo(ctx, pda)
	if err != nil {
		if errors.Is(err, solanarpc.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to get account data: %w", err)
	}
	if account.Value == nil {
		return nil, ErrAccountNotFound
	}

	config, err := DeserializeProgramConfig(account.Value.Data.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize program config: %w", err)
	}
	return config, nil
}

// GetGeoProbeByCode fetches a GeoProbe account by its code.
func (c *Client) GetGeoProbeByCode(ctx context.Context, code string) (*GeoProbe, error) {
	pda, _, err := DeriveGeoProbePDA(c.programID, code)
	if err != nil {
		return nil, fmt.Errorf("failed to derive PDA: %w", err)
	}

	account, err := c.rpc.GetAccountInfo(ctx, pda)
	if err != nil {
		if errors.Is(err, solanarpc.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to get account data: %w", err)
	}
	if account.Value == nil {
		return nil, ErrAccountNotFound
	}

	probe, err := DeserializeGeoProbe(account.Value.Data.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize geo probe: %w", err)
	}
	return probe, nil
}

// GetGeoProbes fetches all GeoProbe accounts for the program.
func (c *Client) GetGeoProbes(ctx context.Context) ([]GeoProbe, error) {
	opts := &solanarpc.GetProgramAccountsOpts{
		Filters: []solanarpc.RPCFilter{
			{
				Memcmp: &solanarpc.RPCFilterMemcmp{
					Offset: 0,
					Bytes:  solana.Base58([]byte{byte(AccountTypeGeoProbe)}),
				},
			},
		},
	}

	accounts, err := c.rpc.GetProgramAccountsWithOpts(ctx, c.programID, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get program accounts: %w", err)
	}

	probes := make([]GeoProbe, 0, len(accounts))
	for _, acct := range accounts {
		probe, err := DeserializeGeoProbe(acct.Account.Data.GetBinary())
		if err != nil {
			c.log.Warn("failed to deserialize geo probe account", "pubkey", acct.Pubkey, "error", err)
			continue
		}
		probes = append(probes, *probe)
	}
	return probes, nil
}

// GetGeoProbeKeys returns the public keys of all GeoProbe accounts without
// fetching full account data. Uses DataSlice to minimize bandwidth, making it
// suitable for polling-based change detection.
func (c *Client) GetGeoProbeKeys(ctx context.Context) ([]solana.PublicKey, error) {
	zero := uint64(0)
	one := uint64(1)
	opts := &solanarpc.GetProgramAccountsOpts{
		Filters: []solanarpc.RPCFilter{
			{
				Memcmp: &solanarpc.RPCFilterMemcmp{
					Offset: 0,
					Bytes:  solana.Base58([]byte{byte(AccountTypeGeoProbe)}),
				},
			},
		},
		DataSlice: &solanarpc.DataSlice{
			Offset: &zero,
			Length: &one,
		},
	}

	accounts, err := c.rpc.GetProgramAccountsWithOpts(ctx, c.programID, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get program account keys: %w", err)
	}

	keys := make([]solana.PublicKey, len(accounts))
	for i, acct := range accounts {
		keys[i] = acct.Pubkey
	}
	return keys, nil
}
