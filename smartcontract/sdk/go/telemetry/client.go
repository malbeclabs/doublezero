package telemetry

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

var (
	ErrAccountNotFound      = errors.New("account not found")
	ErrSamplesBatchTooLarge = fmt.Errorf("samples batch too large, must not exceed %d samples", MaxSamplesPerBatch)
	ErrSamplesAccountFull   = errors.New("samples account is full")
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

	data := account.Value.Data.GetBinary()

	var headerOnlyAccountType DeviceLatencySamplesHeaderOnlyAccountType
	if err := headerOnlyAccountType.Deserialize(data); err != nil {
		return nil, fmt.Errorf("failed to deserialize DeviceLatencySamplesHeaderOnlyAccountType: %w", err)
	}

	switch headerOnlyAccountType.AccountType {
	case AccountTypeDeviceLatencySamplesV0:
		var instance DeviceLatencySamplesV0
		if err := instance.Deserialize(data); err != nil {
			return nil, fmt.Errorf("failed to deserialize DeviceLatencySamples: %w", err)
		}
		return instance.ToV1(), nil
	case AccountTypeDeviceLatencySamples:
		var instance DeviceLatencySamples
		if err := instance.Deserialize(data); err != nil {
			return nil, fmt.Errorf("failed to deserialize DeviceLatencySamples: %w", err)
		}
		return &instance, nil
	default:
		return nil, fmt.Errorf("unknown account type: %d", headerOnlyAccountType.AccountType)
	}
}

func (c *Client) GetDeviceLatencySamplesTail(
	ctx context.Context,
	originDevicePK solana.PublicKey,
	targetDevicePK solana.PublicKey,
	linkPK solana.PublicKey,
	epoch uint64,
	existingMaxIdx int,
) (*DeviceLatencySamplesHeader, int, []uint32, error) {
	pda, _, err := DeriveDeviceLatencySamplesPDA(c.executor.programID, originDevicePK, targetDevicePK, linkPK, epoch)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to derive PDA: %w", err)
	}

	account, err := c.rpc.GetAccountInfo(ctx, pda)
	if err != nil {
		if errors.Is(err, solanarpc.ErrNotFound) {
			return nil, 0, nil, ErrAccountNotFound
		}
		return nil, 0, nil, fmt.Errorf("failed to get account data: %w", err)
	}
	if account.Value == nil {
		return nil, 0, nil, ErrAccountNotFound
	}

	data := account.Value.Data.GetBinary()
	if len(data) < 1 {
		return nil, 0, nil, fmt.Errorf("short account data: %d", len(data))
	}

	switch AccountType(data[0]) {
	case AccountTypeDeviceLatencySamples:
		dec := bin.NewBorshDecoder(data)
		var hdr DeviceLatencySamplesHeader
		if err := dec.Decode(&hdr); err != nil {
			return nil, 0, nil, fmt.Errorf("failed to decode header: %w", err)
		}

		if hdr.NextSampleIndex > MaxDeviceLatencySamplesPerAccount {
			return nil, 0, nil, fmt.Errorf("next sample index %d exceeds max allowed samples %d", hdr.NextSampleIndex, MaxDeviceLatencySamplesPerAccount)
		}

		headerBytes := dec.Position()
		end := int(hdr.NextSampleIndex)

		start := existingMaxIdx + 1
		if start < 0 {
			start = 0
		}
		if start > end {
			start = end
		}
		if start == end {
			return &hdr, start, nil, nil
		}

		need := int(headerBytes) + end*4
		if len(data) < need {
			return nil, 0, nil, fmt.Errorf("short samples region: have %d need %d", len(data), need)
		}

		n := end - start
		out := make([]uint32, n)
		base := int(headerBytes) + start*4
		for i := 0; i < n; i++ {
			off := base + i*4
			out[i] = binary.LittleEndian.Uint32(data[off : off+4])
		}

		return &hdr, start, out, nil

	case AccountTypeDeviceLatencySamplesV0:
		// Keep legacy behavior for now. If V0 still matters and you know its layout,
		// you can add a tail decoder for it too.
		var v0 DeviceLatencySamplesV0
		if err := v0.Deserialize(data); err != nil {
			return nil, 0, nil, fmt.Errorf("failed to deserialize v0: %w", err)
		}
		v1 := v0.ToV1()
		hdr := v1.DeviceLatencySamplesHeader

		end := int(hdr.NextSampleIndex)
		start := existingMaxIdx + 1
		if start < 0 {
			start = 0
		}
		if start > end {
			start = end
		}
		if start == end {
			return &hdr, start, nil, nil
		}

		tail := make([]uint32, end-start)
		copy(tail, v1.Samples[start:end])
		return &hdr, start, tail, nil

	default:
		return nil, 0, nil, fmt.Errorf("unknown account type: %d", data[0])
	}
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
							if code, ok := custom["Custom"].(json.Number); ok {
								switch code.String() {
								case strconv.Itoa(InstructionErrorAccountDoesNotExist):
									return solana.Signature{}, nil, ErrAccountNotFound
								case strconv.Itoa(InstructionErrorAccountSamplesAccountFull):
									return solana.Signature{}, nil, ErrSamplesAccountFull
								}
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

func (c *Client) GetInternetLatencySamples(
	ctx context.Context,
	dataProviderName string,
	originLocationPK solana.PublicKey,
	targetLocationPK solana.PublicKey,
	agentPK solana.PublicKey,
	epoch uint64,
) (*InternetLatencySamples, error) {
	pda, _, err := DeriveInternetLatencySamplesPDA(
		c.executor.programID,
		agentPK,
		dataProviderName,
		originLocationPK,
		targetLocationPK,
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

	var internetLatencySamples InternetLatencySamples
	if err := internetLatencySamples.Deserialize(account.Value.Data.GetBinary()); err != nil {
		return nil, fmt.Errorf("failed to deserialize InternetLatencySamples: %w", err)
	}

	return &internetLatencySamples, nil
}

func (c *Client) InitializeInternetLatencySamples(
	ctx context.Context,
	config InitializeInternetLatencySamplesInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	instruction, err := BuildInitializeInternetLatencySamplesInstruction(c.executor.programID, c.executor.signer.PublicKey(), config)
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

func (c *Client) WriteInternetLatencySamples(
	ctx context.Context,
	config WriteInternetLatencySamplesInstructionConfig,
) (solana.Signature, *solanarpc.GetTransactionResult, error) {

	if len(config.Samples) > MaxSamplesPerBatch {
		return solana.Signature{}, nil, ErrSamplesBatchTooLarge
	}

	instruction, err := BuildWriteInternetLatencySamplesInstruction(c.executor.programID, c.executor.signer.PublicKey(), config)
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
							if code, ok := custom["Custom"].(json.Number); ok {
								switch code.String() {
								case strconv.Itoa(InstructionErrorAccountDoesNotExist):
									return solana.Signature{}, nil, ErrAccountNotFound
								case strconv.Itoa(InstructionErrorAccountSamplesAccountFull):
									return solana.Signature{}, nil, ErrSamplesAccountFull
								}
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
