package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

var (
	ErrAccountNotFound      = errors.New("account not found")
	ErrSamplesBatchTooLarge = fmt.Errorf("samples batch too large, must not exceed %d samples", MaxSamplesPerBatch)
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

func (c *Client) GetDeviceLatencySamples(
	ctx context.Context,
	originDevicePK solana.PublicKey,
	targetDevicePK solana.PublicKey,
	linkPK solana.PublicKey,
	epoch uint64,
) (*DeviceLatencySamples, error) {
	pda, _, err := DeriveDeviceLatencySamplesPDA(
		c.executor.programID,
		originDevicePK,
		targetDevicePK,
		linkPK,
		epoch,
	)
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

	var deviceLatencySamples DeviceLatencySamples
	if err := deviceLatencySamples.Deserialize(bytes.NewReader(account.Value.Data.GetBinary())); err != nil {
		return nil, fmt.Errorf("failed to deserialize DeviceLatencySamples: %w", err)
	}

	return &deviceLatencySamples, nil
}

func (c *Client) InitializeDeviceLatencySamples(
	ctx context.Context,
	config InitializeDeviceLatencySamplesInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	instruction, err := BuildInitializeDeviceLatencySamplesInstruction(c.executor.programID, config)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build instruction: %w", err)
	}

	sig, res, err := c.executor.ExecuteTransaction(ctx, instruction, &ExecuteTransactionOptions{
		// Skip preflight/simulation on this transaction since it creates the account in the
		// instruction itself. Otherwise the preflight will fail with AccountNotFound.
		SkipPreflight: true,
	})
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to execute instruction: %w", err)
	}

	return sig, res, nil
}

func (c *Client) WriteDeviceLatencySamples(
	ctx context.Context,
	config WriteDeviceLatencySamplesInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {

	if len(config.Samples) > MaxSamplesPerBatch {
		return solana.Signature{}, nil, ErrSamplesBatchTooLarge
	}

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
		return solana.Signature{}, nil, fmt.Errorf("failed to execute instruction: %w", err)
	}

	return sig, res, nil
}
