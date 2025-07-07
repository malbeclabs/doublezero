package telemetry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"testing"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestSDK_Telemetry_Client_GetDeviceLatencySamples_HappyPath(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	expected := &telemetry.DeviceLatencySamples{
		DeviceLatencySamplesHeader: telemetry.DeviceLatencySamplesHeader{
			AccountType:                  telemetry.AccountTypeDeviceLatencySamples,
			Epoch:                        42,
			OriginDeviceAgentPK:          solana.NewWallet().PublicKey(),
			OriginDevicePK:               solana.NewWallet().PublicKey(),
			TargetDevicePK:               solana.NewWallet().PublicKey(),
			OriginDeviceLocationPK:       solana.NewWallet().PublicKey(),
			TargetDeviceLocationPK:       solana.NewWallet().PublicKey(),
			LinkPK:                       solana.NewWallet().PublicKey(),
			SamplingIntervalMicroseconds: 100_000,
			StartTimestampMicroseconds:   1_600_000_000,
			NextSampleIndex:              3,
		},
		Samples: []uint32{10, 20, 30},
	}

	mockRPC := &mockRPCClient{
		GetAccountDataIntoFunc: func(_ context.Context, _ solana.PublicKey, out any) error {
			ptr := out.(*[telemetry.DEVICE_LATENCY_SAMPLES_ALLOCATED_SIZE]byte)
			serialized, err := expected.Serialize()
			if err != nil {
				return err
			}
			copy(ptr[:], serialized)
			return nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	got, err := client.GetDeviceLatencySamples(
		context.Background(),
		signer.PublicKey(),
		expected.OriginDevicePK,
		expected.TargetDevicePK,
		expected.LinkPK,
		expected.Epoch,
	)

	require.NoError(t, err)
	require.Equal(t, expected, got)
}

func TestSDK_Telemetry_Client_GetDeviceLatencySamples_AccountNotFound(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetAccountDataIntoFunc: func(_ context.Context, _ solana.PublicKey, _ any) error {
			return solanarpc.ErrNotFound
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	_, err := client.GetDeviceLatencySamples(
		context.Background(),
		signer.PublicKey(),
		solana.NewWallet().PublicKey(),
		solana.NewWallet().PublicKey(),
		solana.NewWallet().PublicKey(),
		42,
	)

	require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
}

func TestSDK_Telemetry_Client_GetDeviceLatencySamples_UnexpectedError(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetAccountDataIntoFunc: func(_ context.Context, _ solana.PublicKey, _ any) error {
			return fmt.Errorf("rpc explosion")
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	_, err := client.GetDeviceLatencySamples(
		context.Background(),
		signer.PublicKey(),
		solana.NewWallet().PublicKey(),
		solana.NewWallet().PublicKey(),
		solana.NewWallet().PublicKey(),
		42,
	)

	require.ErrorContains(t, err, "failed to get account data")
	require.Contains(t, err.Error(), "rpc explosion")
}

func TestSDK_Telemetry_Client_InitializeDeviceLatencySamples_HappyPath(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	expectedSig := solana.MustSignatureFromBase58("5KMdNedHzFX2TZtAj8fKP8pJzzRgU8xydqNBFUD2T2GfbBDPtbA1gJEXFhCRw8vERmkUs8YDQ3cBduzZ8wMEYx7k")

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{
					Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
				},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
			require.False(t, opts.SkipPreflight, "SkipPreflight must be false for initialize account")
			return expectedSig, nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{ConfirmationStatus: solanarpc.ConfirmationStatusFinalized},
				},
			}, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return &solanarpc.GetTransactionResult{Meta: &solanarpc.TransactionMeta{}}, nil
		},
		GetMinimumBalanceForRentExemptionFunc: func(_ context.Context, _ uint64, _ solanarpc.CommitmentType) (uint64, error) {
			return 1000000, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.InitializeDeviceLatencySamplesInstructionConfig{
		AgentPK:                      signer.PublicKey(),
		OriginDevicePK:               solana.NewWallet().PublicKey(),
		TargetDevicePK:               solana.NewWallet().PublicKey(),
		LinkPK:                       solana.NewWallet().PublicKey(),
		Epoch:                        42,
		SamplingIntervalMicroseconds: 500_000,
	}

	sig, tx, err := client.InitializeDeviceLatencySamples(context.Background(), config)

	require.NoError(t, err)
	require.Equal(t, expectedSig, sig)
	require.NotNil(t, tx)
}

func TestSDK_Telemetry_Client_InitializeDeviceLatencySamples_ValidationFails(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	// Invalid: missing AgentPK (validation will fail)
	config := telemetry.InitializeDeviceLatencySamplesInstructionConfig{
		// AgentPK: omitted
		OriginDevicePK:               solana.NewWallet().PublicKey(),
		TargetDevicePK:               solana.NewWallet().PublicKey(),
		LinkPK:                       solana.NewWallet().PublicKey(),
		Epoch:                        42,
		SamplingIntervalMicroseconds: 500_000,
	}

	sig, tx, err := client.InitializeDeviceLatencySamples(context.Background(), config)

	require.ErrorContains(t, err, "agent public key is required")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_InitializeDeviceLatencySamples_ExecutionFails(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{
					Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
				},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return solana.Signature{}, fmt.Errorf("rpc send failure")
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return nil, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return nil, nil
		},
		GetMinimumBalanceForRentExemptionFunc: func(_ context.Context, _ uint64, _ solanarpc.CommitmentType) (uint64, error) {
			return 1000000, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.InitializeDeviceLatencySamplesInstructionConfig{
		AgentPK:                      signer.PublicKey(), // signer must match
		OriginDevicePK:               solana.NewWallet().PublicKey(),
		TargetDevicePK:               solana.NewWallet().PublicKey(),
		LinkPK:                       solana.NewWallet().PublicKey(),
		Epoch:                        42,
		SamplingIntervalMicroseconds: 500_000,
	}

	sig, tx, err := client.InitializeDeviceLatencySamples(context.Background(), config)

	require.ErrorContains(t, err, "failed to initialize account")
	require.ErrorContains(t, err, "rpc send failure")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_WriteDeviceLatencySamples_HappyPath(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	expectedSig := solana.MustSignatureFromBase58("5KMdNedHzFX2TZtAj8fKP8pJzzRgU8xydqNBFUD2T2GfbBDPtbA1gJEXFhCRw8vERmkUs8YDQ3cBduzZ8wMEYx7k")

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{
					Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
				},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
			require.False(t, opts.SkipPreflight, "SkipPreflight must be false for write")
			return expectedSig, nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{ConfirmationStatus: solanarpc.ConfirmationStatusFinalized},
				},
			}, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return &solanarpc.GetTransactionResult{Meta: &solanarpc.TransactionMeta{}}, nil
		},
		GetMinimumBalanceForRentExemptionFunc: func(_ context.Context, _ uint64, _ solanarpc.CommitmentType) (uint64, error) {
			return 1000000, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.WriteDeviceLatencySamplesInstructionConfig{
		AgentPK:                    signer.PublicKey(), // must match signer
		OriginDevicePK:             solana.NewWallet().PublicKey(),
		TargetDevicePK:             solana.NewWallet().PublicKey(),
		LinkPK:                     solana.NewWallet().PublicKey(),
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{1, 2, 3},
	}

	sig, tx, err := client.WriteDeviceLatencySamples(context.Background(), config)

	require.NoError(t, err)
	require.Equal(t, expectedSig, sig)
	require.NotNil(t, tx)
}

func TestSDK_Telemetry_Client_WriteDeviceLatencySamples_PreflightAccountNotFound(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	accountNotFoundRPCError := &jsonrpc.RPCError{
		Code:    -32000,
		Message: "Transaction simulation failed",
		Data: map[string]any{
			"err": "AccountNotFound",
		},
	}

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{
					Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
				},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return solana.Signature{}, accountNotFoundRPCError
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return nil, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return nil, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.WriteDeviceLatencySamplesInstructionConfig{
		AgentPK:                    signer.PublicKey(),
		OriginDevicePK:             solana.NewWallet().PublicKey(),
		TargetDevicePK:             solana.NewWallet().PublicKey(),
		LinkPK:                     solana.NewWallet().PublicKey(),
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{1, 2, 3},
	}

	sig, tx, err := client.WriteDeviceLatencySamples(context.Background(), config)

	require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_WriteDeviceLatencySamples_CustomInstructionErrorAccountNotFound(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	customErr := &jsonrpc.RPCError{
		Code:    -32000,
		Message: "Transaction simulation failed",
		Data: map[string]any{
			"err": map[string]any{
				"InstructionError": []any{
					0,
					map[string]any{
						"Custom": json.Number(strconv.Itoa(int(telemetry.InstructionErrorAccountDoesNotExist))),
					},
				},
			},
		},
	}

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{
					Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
				},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return solana.Signature{}, customErr
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return nil, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return nil, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.WriteDeviceLatencySamplesInstructionConfig{
		AgentPK:                    signer.PublicKey(),
		OriginDevicePK:             solana.NewWallet().PublicKey(),
		TargetDevicePK:             solana.NewWallet().PublicKey(),
		LinkPK:                     solana.NewWallet().PublicKey(),
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{1, 2, 3},
	}

	sig, tx, err := client.WriteDeviceLatencySamples(context.Background(), config)

	require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_WriteDeviceLatencySamples_BuildFails(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{} // should not be called

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	// Intentionally missing AgentPK
	config := telemetry.WriteDeviceLatencySamplesInstructionConfig{
		// AgentPK: missing
		OriginDevicePK:             solana.NewWallet().PublicKey(),
		TargetDevicePK:             solana.NewWallet().PublicKey(),
		LinkPK:                     solana.NewWallet().PublicKey(),
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{10, 20},
	}

	sig, tx, err := client.WriteDeviceLatencySamples(context.Background(), config)

	require.ErrorContains(t, err, "failed to build instruction")
	require.ErrorContains(t, err, "agent public key is required")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_WriteDeviceLatencySamples_ExecutionFails(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{
					Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
				},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return solana.Signature{}, fmt.Errorf("simulated send failure")
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return nil, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return nil, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.WriteDeviceLatencySamplesInstructionConfig{
		AgentPK:                    signer.PublicKey(), // must match signer
		OriginDevicePK:             solana.NewWallet().PublicKey(),
		TargetDevicePK:             solana.NewWallet().PublicKey(),
		LinkPK:                     solana.NewWallet().PublicKey(),
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{10, 20},
	}

	sig, tx, err := client.WriteDeviceLatencySamples(context.Background(), config)

	require.ErrorContains(t, err, "failed to write account")
	require.ErrorContains(t, err, "simulated send failure")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_CreateDeviceLatencySamplesAccount_RentFails(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetMinimumBalanceForRentExemptionFunc: func(_ context.Context, _ uint64, _ solanarpc.CommitmentType) (uint64, error) {
			return 0, fmt.Errorf("rent fetch failed")
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	_, _, _, err := client.CreateDeviceLatencySamplesAccount(
		context.Background(),
		signer.PublicKey(),
		solana.NewWallet().PublicKey(),
		solana.NewWallet().PublicKey(),
		solana.NewWallet().PublicKey(),
		42,
	)

	require.ErrorContains(t, err, "failed to get rent")
}

func TestSDK_Telemetry_Client_CreateDeviceLatencySamplesAccount_HappyPath(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	var derivedAddr solana.PublicKey

	mockRPC := &mockRPCClient{
		GetMinimumBalanceForRentExemptionFunc: func(_ context.Context, space uint64, _ solanarpc.CommitmentType) (uint64, error) {
			require.Greater(t, space, uint64(0))
			return 1_000_000, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			require.NotNil(t, tx)
			require.Greater(t, len(tx.Message.Instructions), 0)

			// Capture the derived address from the instruction
			var found bool
			for _, acct := range tx.Message.AccountKeys {
				if acct.Equals(derivedAddr) {
					found = true
					break
				}
			}
			require.True(t, found, "created account must be in transaction")
			return solana.Signature{}, nil
		},
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: solana.Hash{}},
			}, nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{ConfirmationStatus: solanarpc.ConfirmationStatusFinalized},
				},
			}, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return &solanarpc.GetTransactionResult{Meta: &solanarpc.TransactionMeta{}}, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()
	epoch := uint64(42)

	var err error
	derivedAddr, _, err = telemetry.DeriveDeviceLatencySamplesAddress(
		signer.PublicKey(), programID, origin, target, link, epoch,
	)
	require.NoError(t, err)

	accountAddr, _, _, err := client.CreateDeviceLatencySamplesAccount(
		context.Background(),
		signer.PublicKey(),
		origin,
		target,
		link,
		epoch,
	)

	require.NoError(t, err)
	require.Equal(t, derivedAddr, accountAddr)
}

func TestSDK_Telemetry_Client_InitializeDeviceLatencySamples_CustomInstructionErrorAccountAlreadyInitialized(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	customErr := &jsonrpc.RPCError{
		Code:    -32002,
		Message: "Transaction simulation failed",
		Data: map[string]any{
			"err": map[string]any{
				"InstructionError": []any{
					0,
					map[string]any{
						"Custom": json.Number(strconv.Itoa(int(telemetry.InstructionErrorAccountAlreadyInitialized))),
					},
				},
			},
		},
	}

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{
					Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
				},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return solana.Signature{}, customErr
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return nil, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return nil, nil
		},
		GetMinimumBalanceForRentExemptionFunc: func(_ context.Context, _ uint64, _ solanarpc.CommitmentType) (uint64, error) {
			return 1000000, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.InitializeDeviceLatencySamplesInstructionConfig{
		AgentPK:                      signer.PublicKey(),
		OriginDevicePK:               solana.NewWallet().PublicKey(),
		TargetDevicePK:               solana.NewWallet().PublicKey(),
		LinkPK:                       solana.NewWallet().PublicKey(),
		Epoch:                        42,
		SamplingIntervalMicroseconds: 100_000,
	}

	sig, tx, err := client.InitializeDeviceLatencySamples(context.Background(), config)

	require.ErrorIs(t, err, telemetry.ErrAccountAlreadyInitialized)
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_InitializeDeviceLatencySamples_TransactionMetaErrorAccountAlreadyInitialized(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	expectedSig := solana.MustSignatureFromBase58("5KMdNedHzFX2TZtAj8fKP8pJzzRgU8xydqNBFUD2T2GfbBDPtbA1gJEXFhCRw8vERmkUs8YDQ3cBduzZ8wMEYx7k")

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{
					Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
				},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return expectedSig, nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{ConfirmationStatus: solanarpc.ConfirmationStatusFinalized},
				},
			}, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return &solanarpc.GetTransactionResult{
				Meta: &solanarpc.TransactionMeta{
					Err: map[string]any{
						"InstructionError": []any{
							0,
							map[string]any{
								"Custom": json.Number(strconv.Itoa(int(telemetry.InstructionErrorAccountAlreadyInitialized))),
							},
						},
					},
				},
			}, nil
		},
		GetMinimumBalanceForRentExemptionFunc: func(_ context.Context, _ uint64, _ solanarpc.CommitmentType) (uint64, error) {
			return 1000000, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.InitializeDeviceLatencySamplesInstructionConfig{
		AgentPK:                      signer.PublicKey(),
		OriginDevicePK:               solana.NewWallet().PublicKey(),
		TargetDevicePK:               solana.NewWallet().PublicKey(),
		LinkPK:                       solana.NewWallet().PublicKey(),
		Epoch:                        42,
		SamplingIntervalMicroseconds: 100_000,
	}

	sig, tx, err := client.InitializeDeviceLatencySamples(context.Background(), config)

	require.ErrorIs(t, err, telemetry.ErrAccountAlreadyInitialized)
	require.Equal(t, expectedSig, sig)
	require.NotNil(t, tx)
}

func TestSDK_Telemetry_Client_CreateDeviceLatencySamplesAccount_AlreadyExistsFromLogs(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()
	epoch := uint64(42)

	derivedAddr, _, err := telemetry.DeriveDeviceLatencySamplesAddress(
		signer.PublicKey(), programID, origin, target, link, epoch,
	)
	require.NoError(t, err)

	mockRPC := &mockRPCClient{
		GetMinimumBalanceForRentExemptionFunc: func(_ context.Context, _ uint64, _ solanarpc.CommitmentType) (uint64, error) {
			return 1000000, nil
		},
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: solana.Hash{}},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return solana.MustSignatureFromBase58("5KMdNedHzFX2TZtAj8fKP8pJzzRgU8xydqNBFUD2T2GfbBDPtbA1gJEXFhCRw8vERmkUs8YDQ3cBduzZ8wMEYx7k"), nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{ConfirmationStatus: solanarpc.ConfirmationStatusFinalized},
				},
			}, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return &solanarpc.GetTransactionResult{
				Meta: &solanarpc.TransactionMeta{
					Err:         map[string]any{"InstructionError": []any{"mocked"}},
					LogMessages: []string{"Program log: create account: already in use"},
				},
			}, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	addr, sig, res, err := client.CreateDeviceLatencySamplesAccount(
		context.Background(),
		signer.PublicKey(),
		origin,
		target,
		link,
		epoch,
	)

	require.ErrorIs(t, err, telemetry.ErrAccountAlreadyExists)
	require.Equal(t, derivedAddr, addr)
	require.NotNil(t, sig)
	require.NotNil(t, res)
}
