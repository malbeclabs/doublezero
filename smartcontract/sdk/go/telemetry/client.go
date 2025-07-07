package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

var (
	ErrAccountNotFound           = errors.New("account not found")
	ErrAccountAlreadyInitialized = errors.New("account already initialized")
	ErrAccountAlreadyExists      = errors.New("account already exists")
)

type Client struct {
	log       *slog.Logger
	rpc       RPCClient
	executor  *executor
	programID solana.PublicKey
}

func New(log *slog.Logger, rpc RPCClient, signer *solana.PrivateKey, programID solana.PublicKey) *Client {
	return &Client{
		log:       log,
		rpc:       rpc,
		executor:  NewExecutor(log, rpc, signer, programID),
		programID: programID,
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

func (c *Client) GetDeviceLatencySamples(
	ctx context.Context,
	agentPK solana.PublicKey,
	originDevicePK solana.PublicKey,
	targetDevicePK solana.PublicKey,
	linkPK solana.PublicKey,
	epoch uint64,
) (*DeviceLatencySamples, error) {
	pda, _, err := DeriveDeviceLatencySamplesAddress(
		agentPK,
		c.programID,
		originDevicePK,
		targetDevicePK,
		linkPK,
		epoch,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive PDA: %w", err)
	}

	var data [DEVICE_LATENCY_SAMPLES_ALLOCATED_SIZE]byte
	if err := c.rpc.GetAccountDataInto(ctx, pda, &data); err != nil {
		if errors.Is(err, solanarpc.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to get account data: %w", err)
	}

	deviceLatencySamples, err := DeserializeDeviceLatencySamples(data[:])
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize account data: %w", err)
	}

	return deviceLatencySamples, nil
}

func (c *Client) CreateDeviceLatencySamplesAccount(
	ctx context.Context,
	agentPK solana.PublicKey,
	originDevicePK solana.PublicKey,
	targetDevicePK solana.PublicKey,
	linkPK solana.PublicKey,
	epoch uint64,
) (solana.PublicKey, solana.Signature, *solanarpc.GetTransactionResult, error) {

	accountAddr, seed, err := DeriveDeviceLatencySamplesAddress(agentPK, c.programID, originDevicePK, targetDevicePK, linkPK, epoch)
	if err != nil {
		return solana.PublicKey{}, solana.Signature{}, nil, fmt.Errorf("failed to derive account address: %w", err)
	}

	space := DEVICE_LATENCY_SAMPLES_ALLOCATED_SIZE
	lamports, err := c.rpc.GetMinimumBalanceForRentExemption(ctx, uint64(space), solanarpc.CommitmentFinalized)
	if err != nil {
		return solana.PublicKey{}, solana.Signature{}, nil, fmt.Errorf("failed to get rent: %w", err)
	}

	createIx := system.NewCreateAccountWithSeedInstructionBuilder().
		SetBase(agentPK).
		SetSeed(seed).
		SetLamports(lamports).
		SetSpace(uint64(space)).
		SetOwner(c.programID).
		SetFundingAccount(agentPK).
		SetCreatedAccount(accountAddr).
		Build()

	sig, res, err := c.executor.ExecuteTransaction(ctx, createIx, &ExecuteTransactionOptions{
		// Skip preflight/simulation on this transaction since it creates the account.
		// Otherwise the preflight fails with AccountNotFound.
		SkipPreflight: true,
	})
	if err != nil {
		if errors.Is(err, solanarpc.ErrNotFound) {
			return solana.PublicKey{}, solana.Signature{}, nil, ErrAccountNotFound
		}
		return solana.PublicKey{}, solana.Signature{}, nil, fmt.Errorf("failed to create account: %w", err)
	}
	if res != nil && res.Meta != nil && res.Meta.Err != nil {
		for _, msg := range res.Meta.LogMessages {
			msg = strings.ToLower(msg)
			if strings.Contains(msg, "create account") && strings.Contains(msg, "already in use") {
				return accountAddr, sig, res, ErrAccountAlreadyExists
			}
		}
		return accountAddr, sig, res, fmt.Errorf("failed to create account: %v", res.Meta.Err)
	}

	return accountAddr, sig, res, nil
}

func (c *Client) InitializeDeviceLatencySamples(
	ctx context.Context,
	config InitializeDeviceLatencySamplesInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	if err := config.Validate(); err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to validate config: %w", err)
	}

	instruction, err := BuildInitializeDeviceLatencySamplesInstruction(c.executor.programID, config)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build instruction: %w", err)
	}

	sig, res, err := c.executor.ExecuteTransaction(ctx, instruction, nil)
	if err != nil {
		var rpcErr *jsonrpc.RPCError
		if errors.As(err, &rpcErr) {
			if data, ok := rpcErr.Data.(map[string]any); ok {
				switch v := data["err"].(type) {
				case string:
					if v == "AccountNotFound" {
						return solana.Signature{}, nil, ErrAccountNotFound
					}
				case map[string]any:
					if ie, ok := v["InstructionError"].([]any); ok && len(ie) == 2 {
						if custom, ok := ie[1].(map[string]any); ok {
							if code, ok := custom["Custom"].(json.Number); ok && code.String() == strconv.Itoa(int(InstructionErrorAccountAlreadyInitialized)) {
								return solana.Signature{}, nil, ErrAccountAlreadyInitialized
							}
						}
					}
				}
			}
		}
		return sig, res, fmt.Errorf("failed to initialize account: %w", err)
	}
	if res != nil && res.Meta != nil && res.Meta.Err != nil {
		if data, ok := res.Meta.Err.(map[string]any); ok {
			if ie, ok := data["InstructionError"].([]any); ok && len(ie) == 2 {
				if custom, ok := ie[1].(map[string]any); ok {
					if code, ok := custom["Custom"].(json.Number); ok && code.String() == strconv.Itoa(int(InstructionErrorAccountAlreadyInitialized)) {
						return sig, res, ErrAccountAlreadyInitialized
					}
				}
			}
		}
		return sig, res, fmt.Errorf("failed to initialize account: %v", res.Meta.Err)
	}

	return sig, res, nil
}

func (c *Client) WriteDeviceLatencySamples(
	ctx context.Context,
	config WriteDeviceLatencySamplesInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	instruction, err := BuildWriteDeviceLatencySamplesInstruction(c.executor.programID, config)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build instruction: %w", err)
	}

	sig, res, err := c.executor.ExecuteTransaction(ctx, instruction, nil)
	if err != nil {
		var rpcErr *jsonrpc.RPCError
		if errors.As(err, &rpcErr) {
			if data, ok := rpcErr.Data.(map[string]any); ok {
				switch v := data["err"].(type) {
				case string:
					if v == "AccountNotFound" {
						return solana.Signature{}, nil, ErrAccountNotFound
					}
				case map[string]any:
					if ie, ok := v["InstructionError"].([]any); ok && len(ie) == 2 {
						if custom, ok := ie[1].(map[string]any); ok {
							if code, ok := custom["Custom"].(json.Number); ok && code.String() == strconv.Itoa(InstructionErrorAccountDoesNotExist) {
								return solana.Signature{}, nil, ErrAccountNotFound
							}
						}
					}
				}
			}
		}
		return solana.Signature{}, nil, fmt.Errorf("failed to write account: %w", err)
	}
	if res != nil && res.Meta != nil && res.Meta.Err != nil {
		return sig, res, fmt.Errorf("failed to write account: %v", res.Meta.Err)
	}

	return sig, res, nil
}
