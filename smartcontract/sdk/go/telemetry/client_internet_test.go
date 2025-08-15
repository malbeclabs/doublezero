package telemetry_test

import (
	"bytes"
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

func TestSDK_Telemetry_Client_GetInternetLatencySamples_HappyPath(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	expected := &telemetry.InternetLatencySamples{
		InternetLatencySamplesHeader: telemetry.InternetLatencySamplesHeader{
			AccountType:                  telemetry.AccountTypeInternetLatencySamples,
			Epoch:                        42,
			DataProviderName:             "test-data-provider-1",
			OriginExchangePK:             solana.NewWallet().PublicKey(),
			TargetExchangePK:             solana.NewWallet().PublicKey(),
			SamplingIntervalMicroseconds: 100_000,
			StartTimestampMicroseconds:   1_600_000_000,
			NextSampleIndex:              3,
		},
		Samples: []uint32{10, 20, 30},
	}

	mockRPC := &mockRPCClient{
		GetAccountInfoFunc: func(_ context.Context, _ solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			buf := new(bytes.Buffer)
			if err := expected.Serialize(buf); err != nil {
				return nil, fmt.Errorf("mock serialize: %w", err)
			}
			return &solanarpc.GetAccountInfoResult{
				Value: &solanarpc.Account{
					Data: solanarpc.DataBytesOrJSONFromBytes(buf.Bytes()),
				},
			}, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	got, err := client.GetInternetLatencySamples(
		context.Background(),
		expected.DataProviderName,
		expected.OriginExchangePK,
		expected.TargetExchangePK,
		signer.PublicKey(),
		expected.Epoch,
	)

	require.NoError(t, err)
	require.Equal(t, expected, got)
}

func TestSDK_Telemetry_Client_GetInternetLatencySamples_AccountNotFound(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetAccountInfoFunc: func(_ context.Context, _ solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			return nil, solanarpc.ErrNotFound
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	_, err := client.GetInternetLatencySamples(
		context.Background(),
		"test-data-provider-1",
		solana.NewWallet().PublicKey(),
		solana.NewWallet().PublicKey(),
		signer.PublicKey(),
		42,
	)

	require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
}

func TestSDK_Telemetry_Client_GetInternetLatencySamples_UnexpectedError(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetAccountInfoFunc: func(_ context.Context, _ solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			return nil, fmt.Errorf("rpc explosion")
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	_, err := client.GetInternetLatencySamples(
		context.Background(),
		"test-data-provider-1",
		solana.NewWallet().PublicKey(),
		solana.NewWallet().PublicKey(),
		signer.PublicKey(),
		42,
	)

	require.ErrorContains(t, err, "failed to get account data")
	require.Contains(t, err.Error(), "rpc explosion")
}

func TestSDK_Telemetry_Client_InitializeInternetLatencySamples_HappyPath(t *testing.T) {
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
			require.True(t, opts.SkipPreflight, "SkipPreflight must be true for initialize")
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
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.InitializeInternetLatencySamplesInstructionConfig{
		OriginExchangePK:             solana.NewWallet().PublicKey(),
		TargetExchangePK:             solana.NewWallet().PublicKey(),
		DataProviderName:             "test-data-provider-1",
		Epoch:                        42,
		SamplingIntervalMicroseconds: 500_000,
	}

	sig, tx, err := client.InitializeInternetLatencySamples(context.Background(), config)

	require.NoError(t, err)
	require.Equal(t, expectedSig, sig)
	require.NotNil(t, tx)
}

func TestSDK_Telemetry_Client_InitializeInternetLatencySamples_BuildFails(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{} // won't be called

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.InitializeInternetLatencySamplesInstructionConfig{
		OriginExchangePK:             solana.PublicKey{},
		TargetExchangePK:             solana.NewWallet().PublicKey(),
		DataProviderName:             "test-data-provider-1",
		Epoch:                        42,
		SamplingIntervalMicroseconds: 500_000,
	}

	sig, tx, err := client.InitializeInternetLatencySamples(context.Background(), config)

	require.ErrorContains(t, err, "failed to build instruction")
	require.Contains(t, err.Error(), "origin exchange public key is required")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_InitializeInternetLatencySamples_ExecutionFails(t *testing.T) {
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
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.InitializeInternetLatencySamplesInstructionConfig{
		OriginExchangePK:             solana.NewWallet().PublicKey(),
		TargetExchangePK:             solana.NewWallet().PublicKey(),
		DataProviderName:             "test-data-provider-1",
		Epoch:                        42,
		SamplingIntervalMicroseconds: 500_000,
	}

	sig, tx, err := client.InitializeInternetLatencySamples(context.Background(), config)

	require.ErrorContains(t, err, "failed to execute instruction")
	require.Contains(t, err.Error(), "rpc send failure")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_WriteInternetLatencySamples_HappyPath(t *testing.T) {
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
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.WriteInternetLatencySamplesInstructionConfig{
		OriginExchangePK:           solana.NewWallet().PublicKey(),
		TargetExchangePK:           solana.NewWallet().PublicKey(),
		DataProviderName:           "test-data-provider-1",
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{1, 2, 3},
	}

	sig, tx, err := client.WriteInternetLatencySamples(context.Background(), config)

	require.NoError(t, err)
	require.Equal(t, expectedSig, sig)
	require.NotNil(t, tx)
}

func TestSDK_Telemetry_Client_WriteInternetLatencySamples_SamplesBatchTooLarge(t *testing.T) {
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
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.WriteInternetLatencySamplesInstructionConfig{
		OriginExchangePK:           solana.NewWallet().PublicKey(),
		TargetExchangePK:           solana.NewWallet().PublicKey(),
		DataProviderName:           "test-data-provider-1",
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    make([]uint32, telemetry.MaxSamplesPerBatch+1),
	}

	sig, tx, err := client.WriteInternetLatencySamples(context.Background(), config)

	require.ErrorIs(t, err, telemetry.ErrSamplesBatchTooLarge)
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}
func TestSDK_Telemetry_Client_WriteInternetLatencySamples_PreflightAccountNotFound(t *testing.T) {
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

	config := telemetry.WriteInternetLatencySamplesInstructionConfig{
		OriginExchangePK:           solana.NewWallet().PublicKey(),
		TargetExchangePK:           solana.NewWallet().PublicKey(),
		DataProviderName:           "test-data-provider-1",
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{1, 2, 3},
	}

	sig, tx, err := client.WriteInternetLatencySamples(context.Background(), config)

	require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_WriteInternetLatencySamples_CustomInstructionErrorAccountNotFound(t *testing.T) {
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
						"Custom": json.Number(strconv.Itoa(telemetry.InstructionErrorAccountDoesNotExist)),
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

	config := telemetry.WriteInternetLatencySamplesInstructionConfig{
		OriginExchangePK:           solana.NewWallet().PublicKey(),
		TargetExchangePK:           solana.NewWallet().PublicKey(),
		DataProviderName:           "test-data-provider-1",
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{1, 2, 3},
	}

	sig, tx, err := client.WriteInternetLatencySamples(context.Background(), config)

	require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_WriteInternetLatencySamples_BuildFails(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{} // should not be called

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	// Intentionally missing OracleAgentPK
	config := telemetry.WriteInternetLatencySamplesInstructionConfig{
		OriginExchangePK:           solana.PublicKey{},
		TargetExchangePK:           solana.PublicKey{},
		DataProviderName:           "test-data-provider-1",
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{10, 20},
	}

	sig, tx, err := client.WriteInternetLatencySamples(context.Background(), config)

	require.ErrorContains(t, err, "failed to build instruction")
	require.Contains(t, err.Error(), "origin exchange public key is required")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_WriteInternetLatencySamples_ExecutionFails(t *testing.T) {
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

	config := telemetry.WriteInternetLatencySamplesInstructionConfig{
		OriginExchangePK:           solana.NewWallet().PublicKey(),
		TargetExchangePK:           solana.NewWallet().PublicKey(),
		DataProviderName:           "test-data-provider-1",
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{10, 20},
	}

	sig, tx, err := client.WriteInternetLatencySamples(context.Background(), config)

	require.ErrorContains(t, err, "failed to execute instruction")
	require.Contains(t, err.Error(), "simulated send failure")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}

func TestSDK_Telemetry_Client_WriteInternetLatencySamples_CustomInstructionErrorSamplesAccountFull(t *testing.T) {
	t.Parallel()
	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	fullErr := &jsonrpc.RPCError{
		Code:    -32000,
		Message: "Simulation failed",
		Data: map[string]any{
			"err": map[string]any{
				"InstructionError": []any{
					0,
					map[string]any{
						"Custom": json.Number(strconv.Itoa(telemetry.InstructionErrorAccountSamplesAccountFull)),
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
			return solana.Signature{}, fullErr
		},
		GetSignatureStatusesFunc: func(context.Context, bool, ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return nil, nil
		},
		GetTransactionFunc: func(context.Context, solana.Signature, *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return nil, nil
		},
	}

	client := telemetry.New(slog.Default(), mockRPC, &signer, programID)

	config := telemetry.WriteInternetLatencySamplesInstructionConfig{
		OriginExchangePK:           solana.NewWallet().PublicKey(),
		TargetExchangePK:           solana.NewWallet().PublicKey(),
		DataProviderName:           "test-data-provider-1",
		Epoch:                      42,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{10},
	}

	sig, tx, err := client.WriteInternetLatencySamples(context.Background(), config)
	require.ErrorIs(t, err, telemetry.ErrSamplesAccountFull)
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, tx)
}
