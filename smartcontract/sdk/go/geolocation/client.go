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
	log      *slog.Logger
	rpc      RPCClient
	executor *executor
}

func New(log *slog.Logger, rpc RPCClient, signer *solana.PrivateKey, programID solana.PublicKey) *Client {
	return &Client{
		log:      log,
		rpc:      rpc,
		executor: NewExecutor(log, rpc, signer, programID),
	}
}

func (c *Client) ProgramID() solana.PublicKey {
	if c.executor == nil {
		return solana.PublicKey{}
	}
	return c.executor.programID
}

func (c *Client) Signer() *solana.PrivateKey {
	if c.executor == nil {
		return nil
	}
	return c.executor.signer
}

// GetProgramConfig fetches the GeolocationProgramConfig account.
func (c *Client) GetProgramConfig(ctx context.Context) (*GeolocationProgramConfig, error) {
	pda, _, err := DeriveProgramConfigPDA(c.executor.programID)
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
	pda, _, err := DeriveGeoProbePDA(c.executor.programID, code)
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

	accounts, err := c.rpc.GetProgramAccountsWithOpts(ctx, c.executor.programID, opts)
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

// InitProgramConfig initializes the geolocation program config.
func (c *Client) InitProgramConfig(
	ctx context.Context,
	config InitProgramConfigInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	instruction, err := BuildInitProgramConfigInstruction(c.executor.programID, config)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build instruction: %w", err)
	}

	sig, res, err := c.executor.ExecuteTransaction(ctx, instruction, &ExecuteTransactionOptions{
		SkipPreflight: true,
	})
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to execute instruction: %w", err)
	}
	return sig, res, nil
}

// UpdateProgramConfig updates the geolocation program config.
func (c *Client) UpdateProgramConfig(
	ctx context.Context,
	config UpdateProgramConfigInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	instruction, err := BuildUpdateProgramConfigInstruction(c.executor.programID, config)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build instruction: %w", err)
	}

	sig, res, err := c.executor.ExecuteTransaction(ctx, instruction, nil)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to execute instruction: %w", err)
	}
	return sig, res, nil
}

// CreateGeoProbe creates a new GeoProbe account.
func (c *Client) CreateGeoProbe(
	ctx context.Context,
	config CreateGeoProbeInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	instruction, err := BuildCreateGeoProbeInstruction(c.executor.programID, config)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build instruction: %w", err)
	}

	sig, res, err := c.executor.ExecuteTransaction(ctx, instruction, &ExecuteTransactionOptions{
		SkipPreflight: true,
	})
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to execute instruction: %w", err)
	}
	return sig, res, nil
}

// UpdateGeoProbe updates an existing GeoProbe account.
func (c *Client) UpdateGeoProbe(
	ctx context.Context,
	config UpdateGeoProbeInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	instruction, err := BuildUpdateGeoProbeInstruction(c.executor.programID, config)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build instruction: %w", err)
	}

	sig, res, err := c.executor.ExecuteTransaction(ctx, instruction, nil)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to execute instruction: %w", err)
	}
	return sig, res, nil
}

// DeleteGeoProbe deletes a GeoProbe account.
func (c *Client) DeleteGeoProbe(
	ctx context.Context,
	config DeleteGeoProbeInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	instruction, err := BuildDeleteGeoProbeInstruction(c.executor.programID, config)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build instruction: %w", err)
	}

	sig, res, err := c.executor.ExecuteTransaction(ctx, instruction, nil)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to execute instruction: %w", err)
	}
	return sig, res, nil
}

// AddParentDevice adds a parent device to a GeoProbe.
func (c *Client) AddParentDevice(
	ctx context.Context,
	config AddParentDeviceInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	instruction, err := BuildAddParentDeviceInstruction(c.executor.programID, config)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build instruction: %w", err)
	}

	sig, res, err := c.executor.ExecuteTransaction(ctx, instruction, &ExecuteTransactionOptions{
		SkipPreflight: true,
	})
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to execute instruction: %w", err)
	}
	return sig, res, nil
}

// RemoveParentDevice removes a parent device from a GeoProbe.
func (c *Client) RemoveParentDevice(
	ctx context.Context,
	config RemoveParentDeviceInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	instruction, err := BuildRemoveParentDeviceInstruction(c.executor.programID, config)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build instruction: %w", err)
	}

	sig, res, err := c.executor.ExecuteTransaction(ctx, instruction, nil)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to execute instruction: %w", err)
	}
	return sig, res, nil
}
