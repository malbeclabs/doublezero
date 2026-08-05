package telemetry_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestSDK_Telemetry_Executor_ExecuteTransaction(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	var sig solana.Signature
	copy(sig[:], []byte("fake-sig-0000000000000000000000000000000")[:])

	blockhash := solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ")

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{
					Blockhash: blockhash,
				},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return sig, nil
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
				Meta: &solanarpc.TransactionMeta{},
			}, nil
		},
	}

	exec := telemetry.NewExecutor(log, mockRPC, &signer, programID)

	instruction := solana.NewInstruction(
		programID,
		solana.AccountMetaSlice{},
		[]byte{1, 2, 3},
	)

	ctx := t.Context()
	opts := &telemetry.ExecuteTransactionOptions{}
	gotSig, res, err := exec.ExecuteTransaction(ctx, instruction, opts)

	require.NoError(t, err)
	require.Equal(t, sig, gotSig)
	require.NotNil(t, res)
}

func TestSDK_Telemetry_Executor_MissingSigner(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	mockRPC := &mockRPCClient{} // doesn't matter, should return early

	exec := telemetry.NewExecutor(log, mockRPC, nil, programID)

	instruction := solana.NewInstruction(
		programID,
		solana.AccountMetaSlice{},
		[]byte{1, 2, 3},
	)

	sig, res, err := exec.ExecuteTransaction(t.Context(), instruction, nil)

	require.ErrorIs(t, err, telemetry.ErrNoPrivateKey)
	require.Empty(t, sig)
	require.Nil(t, res)
}

func TestSDK_Telemetry_Executor_MissingProgramID(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	zeroProgramID := solana.PublicKey{} // zero value
	mockRPC := &mockRPCClient{}

	exec := telemetry.NewExecutor(log, mockRPC, &signer, zeroProgramID)

	instruction := solana.NewInstruction(
		solana.NewWallet().PublicKey(), // instruction still needs a non-zero program
		solana.AccountMetaSlice{},
		[]byte{1, 2, 3},
	)

	sig, res, err := exec.ExecuteTransaction(t.Context(), instruction, nil)

	require.ErrorIs(t, err, telemetry.ErrNoProgramID)
	require.Empty(t, sig)
	require.Nil(t, res)
}

func TestSDK_Telemetry_Executor_GetLatestBlockhashError(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return nil, errors.New("rpc unavailable")
		},
	}

	exec := telemetry.NewExecutor(log, mockRPC, &signer, programID)

	instruction := solana.NewInstruction(
		programID,
		solana.AccountMetaSlice{},
		[]byte{1, 2, 3},
	)

	sig, res, err := exec.ExecuteTransaction(t.Context(), instruction, nil)

	require.ErrorContains(t, err, "failed to get latest blockhash")
	require.Empty(t, sig)
	require.Nil(t, res)
}

func TestSDK_Telemetry_Executor_SendFails(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	blockhash := solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ")

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: blockhash},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return solana.Signature{}, errors.New("rpc send error")
		},
	}

	exec := telemetry.NewExecutor(log, mockRPC, &signer, programID)

	instruction := solana.NewInstruction(
		programID,
		solana.AccountMetaSlice{
			{PublicKey: signer.PublicKey(), IsSigner: true, IsWritable: true},
		},
		[]byte{1, 2, 3},
	)

	sig, res, err := exec.ExecuteTransaction(t.Context(), instruction, nil)

	require.ErrorContains(t, err, "failed to send transaction")
	require.Empty(t, sig)
	require.Nil(t, res)
}

func TestSDK_Telemetry_Executor_SignatureNeverVisible(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	signerPub := signer.PublicKey()
	programID := solana.NewWallet().PublicKey()
	blockhash := solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ")

	var returnedSig solana.Signature

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: blockhash},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			if len(tx.Signatures) == 0 {
				t.Fatal("transaction was not signed")
			}
			returnedSig = tx.Signatures[0]
			return returnedSig, nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			// Simulate that the cluster never sees the signature
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{nil},
			}, nil
		},
		// Not used in this test but required to satisfy interface
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return nil, errors.New("not called")
		},
	}

	exec := telemetry.NewExecutor(log, mockRPC, &signer, programID, telemetry.WithWaitForVisibleTimeout(500*time.Millisecond))

	instruction := solana.NewInstruction(
		programID,
		solana.AccountMetaSlice{
			{PublicKey: signerPub, IsSigner: true, IsWritable: true},
		},
		[]byte{1, 2, 3},
	)

	ctx := t.Context()
	opts := &telemetry.ExecuteTransactionOptions{SkipPreflight: false}
	gotSig, res, err := exec.ExecuteTransaction(ctx, instruction, opts)

	require.ErrorContains(t, err, "transaction dropped or rejected before cluster saw it")
	require.Equal(t, solana.Signature{}, gotSig, "executor returns zero sig on error (by design)")
	require.NotEqual(t, solana.Signature{}, returnedSig, "the signed tx should still contain a real signature")
	require.Nil(t, res)
}

func TestSDK_Telemetry_Executor_TransactionNeverFinalized(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	signerPub := signer.PublicKey()
	programID := solana.NewWallet().PublicKey()
	blockhash := solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ")

	sigChan := make(chan solana.Signature, 1)

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: blockhash},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			if len(tx.Signatures) == 0 {
				t.Fatal("tx.Signatures is empty")
			}
			sigChan <- tx.Signatures[0]
			return tx.Signatures[0], nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, sigs ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{
						ConfirmationStatus: solanarpc.ConfirmationStatusConfirmed, // <- never finalized
					},
				},
			}, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			t.Fatal("GetTransaction should not be called if not finalized")
			return nil, nil
		},
	}

	exec := telemetry.NewExecutor(log, mockRPC, &signer, programID)

	instruction := solana.NewInstruction(
		programID,
		solana.AccountMetaSlice{
			{PublicKey: signerPub, IsSigner: true, IsWritable: true},
		},
		[]byte{1, 2, 3},
	)

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	opts := &telemetry.ExecuteTransactionOptions{}
	sig, res, err := exec.ExecuteTransaction(ctx, instruction, opts)

	require.Error(t, err)
	require.Contains(t, err.Error(), "context deadline exceeded")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, res)
}

// TestSDK_Telemetry_Executor_FinalizedWithProgramError covers the chi-dn-dzd4 case
// (malbeclabs/infra#1703): an InitializeDeviceLatencySamples the program rejected with
// UnauthorizedAgent (0x3e9) still finalizes, and reporting it as success left the submitter
// re-initializing an account that never existed with nothing in the log naming the cause.
func TestSDK_Telemetry_Executor_FinalizedWithProgramError(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	signerPub := signer.PublicKey()
	programID := solana.NewWallet().PublicKey()
	blockhash := solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ")

	// UnauthorizedAgent = 1001 = 0x3e9, as the RPC renders it.
	txErr := map[string]any{"InstructionError": []any{0, map[string]any{"Custom": 1001}}}
	logMessages := []string{
		"Program " + programID.String() + " invoke [1]",
		"Program log: Instruction: InitializeDeviceLatencySamples",
		"Program log: Agent BA14eqpRNmkcQhjsH5abfvaUxRi7RcGGuQVeQuJdwPZc is not authorized for origin device FYkmttUmox6kZVjVNCATXEdGt3bfLicn5fJ8fnGfF4fZ",
		"Program " + programID.String() + " failed: custom program error: 0x3e9",
	}

	tests := []struct {
		name       string
		statusErr  any
		metaErr    any
		getTxFails bool
		wantLogs   []string
	}{
		{
			name:      "reported on the signature status",
			statusErr: txErr,
			metaErr:   txErr,
			wantLogs:  logMessages,
		},
		{
			// A node that returns a clean status still reports the rejection on the transaction.
			name:     "reported only on the transaction meta",
			metaErr:  txErr,
			wantLogs: logMessages,
		},
		{
			// The rejection still surfaces when the logs cannot be fetched to explain it.
			name:       "logs unavailable",
			statusErr:  txErr,
			getTxFails: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockRPC := &mockRPCClient{
				GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
					return &solanarpc.GetLatestBlockhashResult{
						Value: &solanarpc.LatestBlockhashResult{Blockhash: blockhash},
					}, nil
				},
				SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
					return tx.Signatures[0], nil
				},
				GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
					return &solanarpc.GetSignatureStatusesResult{
						Value: []*solanarpc.SignatureStatusesResult{
							{
								ConfirmationStatus: solanarpc.ConfirmationStatusFinalized,
								Err:                tt.statusErr,
							},
						},
					}, nil
				},
				GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
					if tt.getTxFails {
						return nil, errors.New("rpc unavailable")
					}
					return &solanarpc.GetTransactionResult{
						Meta: &solanarpc.TransactionMeta{
							Err:         tt.metaErr,
							LogMessages: logMessages,
						},
					}, nil
				},
			}

			exec := telemetry.NewExecutor(log, mockRPC, &signer, programID)

			instruction := solana.NewInstruction(
				programID,
				solana.AccountMetaSlice{
					{PublicKey: signerPub, IsSigner: true, IsWritable: true},
				},
				[]byte{1, 2, 3},
			)

			sig, res, err := exec.ExecuteTransaction(t.Context(), instruction, &telemetry.ExecuteTransactionOptions{SkipPreflight: true})

			var programErr *telemetry.ProgramError
			require.ErrorAs(t, err, &programErr, "a finalized rejection must not be reported as success")
			require.Equal(t, txErr, programErr.Err)
			require.Equal(t, tt.wantLogs, programErr.Logs)
			require.Equal(t, solana.Signature{}, sig)
			require.Nil(t, res)

			// The custom error code identifies the rejection even when the logs are missing.
			require.ErrorContains(t, err, "Custom:1001")
			if len(tt.wantLogs) > 0 {
				// The program's own explanation reaches the message, without the runtime's
				// invoke/failed boilerplate or the instruction-name echo.
				require.ErrorContains(t, err, "is not authorized for origin device")
				require.NotContains(t, err.Error(), "Instruction: InitializeDeviceLatencySamples")
				require.NotContains(t, err.Error(), "invoke [1]")
			}
		})
	}
}

func TestSDK_Telemetry_ProgramError_CustomErrorCode(t *testing.T) {
	t.Parallel()

	customErr := func(code any) map[string]any {
		return map[string]any{"InstructionError": []any{0, map[string]any{"Custom": code}}}
	}

	tests := []struct {
		name string
		err  any
		want uint32
		ok   bool
	}{
		// Which numeric type the code arrives as depends on the decoder behind the RPC client.
		{name: "json.Number", err: customErr(json.Number("1001")), want: 1001, ok: true},
		{name: "float64", err: customErr(float64(1006)), want: 1006, ok: true},
		{name: "int", err: customErr(1011), want: 1011, ok: true},
		{name: "uint64", err: customErr(uint64(1010)), want: 1010, ok: true},
		{name: "not a custom error", err: map[string]any{"InstructionError": []any{0, "InvalidAccountData"}}},
		{name: "runtime error with no instruction error", err: "BlockhashNotFound"},
		{name: "nil", err: nil},
		{name: "negative code", err: customErr(float64(-1))},
		{name: "code beyond uint32", err: customErr(json.Number("4294967296"))},
		{name: "unparseable code", err: customErr(json.Number("not-a-number"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			code, ok := (&telemetry.ProgramError{Err: tt.err}).CustomErrorCode()
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, code)
		})
	}
}

// The reason a failure happened is not always a "Program log:" line. A native program reached
// through CPI — the system program, when an agent cannot fund the account it is creating — logs it
// unprefixed, and dropping those lines would leave only an opaque error code.
func TestSDK_Telemetry_ProgramError_ProgramLogMessages(t *testing.T) {
	t.Parallel()

	programErr := &telemetry.ProgramError{
		Err: map[string]any{"InstructionError": []any{0, map[string]any{"Custom": 1}}},
		Logs: []string{
			"Program TeLeMetRy1111111111111111111111111111111111 invoke [1]",
			"Program log: Instruction: InitializeDeviceLatencySamples",
			"Program log: Processing InitializeDeviceLatencySamples",
			"Program 11111111111111111111111111111111 invoke [2]",
			"Transfer: insufficient lamports 0, need 890880",
			"Program 11111111111111111111111111111111 failed: custom program error: 0x1",
			"Program TeLeMetRy1111111111111111111111111111111111 consumed 4242 of 200000 compute units",
			"Program TeLeMetRy1111111111111111111111111111111111 failed: custom program error: 0x1",
		},
	}

	require.Equal(t, []string{
		"Processing InitializeDeviceLatencySamples",
		"Transfer: insufficient lamports 0, need 890880",
	}, programErr.ProgramLogMessages())

	// And the same lines reach anyone who only prints the error.
	require.Contains(t, programErr.Error(), "insufficient lamports")
	require.NotContains(t, programErr.Error(), "compute units")
	require.NotContains(t, programErr.Error(), "Instruction: InitializeDeviceLatencySamples")
}

func TestSDK_Telemetry_Executor_FinalizedButMissingTransactionMeta(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	signerPub := signer.PublicKey()
	programID := solana.NewWallet().PublicKey()
	blockhash := solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ")

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: blockhash},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			if len(tx.Signatures) == 0 {
				t.Fatal("tx.Signatures is empty")
			}
			return tx.Signatures[0], nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{
						ConfirmationStatus: solanarpc.ConfirmationStatusFinalized,
					},
				},
			}, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			// Simulate finalized tx, but missing metadata
			return &solanarpc.GetTransactionResult{
				Meta: nil,
			}, nil
		},
	}

	exec := telemetry.NewExecutor(log, mockRPC, &signer, programID)

	instruction := solana.NewInstruction(
		programID,
		solana.AccountMetaSlice{
			{PublicKey: signerPub, IsSigner: true, IsWritable: true},
		},
		[]byte{1, 2, 3},
	)

	ctx := t.Context()
	opts := &telemetry.ExecuteTransactionOptions{}
	sig, res, err := exec.ExecuteTransaction(ctx, instruction, opts)

	require.ErrorContains(t, err, "transaction not found or missing metadata")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, res)
}

func TestSDK_Telemetry_Executor_EmptySignatureStatusesSlice(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	signerPub := signer.PublicKey()
	blockhash := solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ")

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: blockhash},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return tx.Signatures[0], nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			// Empty Value slice (simulate RPC regression)
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{},
			}, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			t.Fatal("should not reach GetTransaction when status value is empty")
			return nil, nil
		},
	}

	exec := telemetry.NewExecutor(log, mockRPC, &signer, programID, telemetry.WithWaitForVisibleTimeout(500*time.Millisecond))

	instruction := solana.NewInstruction(
		programID,
		solana.AccountMetaSlice{
			{PublicKey: signerPub, IsSigner: true, IsWritable: true},
		},
		[]byte{42},
	)

	ctx := t.Context()
	opts := &telemetry.ExecuteTransactionOptions{}
	sig, res, err := exec.ExecuteTransaction(ctx, instruction, opts)

	require.ErrorContains(t, err, "transaction dropped or rejected before cluster saw it")
	require.ErrorContains(t, err, "signature not found after wait")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, res)
}

func TestSDK_Telemetry_Executor_SignatureStatusesContainsNil(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	signerPub := signer.PublicKey()
	programID := solana.NewWallet().PublicKey()
	blockhash := solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ")

	var returnedSig solana.Signature

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: blockhash},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			if len(tx.Signatures) == 0 {
				t.Fatal("tx.Signatures is empty")
			}
			returnedSig = tx.Signatures[0]
			return returnedSig, nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{nil}, // <- this is the edge case
			}, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			t.Fatal("GetTransaction should not be called when signature never visible")
			return nil, nil
		},
	}

	exec := telemetry.NewExecutor(log, mockRPC, &signer, programID, telemetry.WithWaitForVisibleTimeout(500*time.Millisecond))

	instruction := solana.NewInstruction(
		programID,
		solana.AccountMetaSlice{
			{PublicKey: signerPub, IsSigner: true, IsWritable: true},
		},
		[]byte("abc"),
	)

	ctx := t.Context()
	opts := &telemetry.ExecuteTransactionOptions{}
	sig, res, err := exec.ExecuteTransaction(ctx, instruction, opts)

	require.ErrorContains(t, err, "transaction dropped or rejected before cluster saw it")
	require.NotEqual(t, solana.Signature{}, returnedSig)
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, res)
}

func TestSDK_Telemetry_Executor_FinalizedButGetTransactionNil(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	signerPub := signer.PublicKey()
	programID := solana.NewWallet().PublicKey()
	blockhash := solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ")

	mockRPC := &mockRPCClient{
		GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: blockhash},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			if len(tx.Signatures) == 0 {
				t.Fatal("tx.Signatures is empty")
			}
			return tx.Signatures[0], nil
		},
		GetSignatureStatusesFunc: func(_ context.Context, _ bool, _ ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{ConfirmationStatus: solanarpc.ConfirmationStatusFinalized},
				},
			}, nil
		},
		GetTransactionFunc: func(_ context.Context, _ solana.Signature, _ *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return nil, nil // ← simulate node RPC dropping the data
		},
	}

	exec := telemetry.NewExecutor(log, mockRPC, &signer, programID)

	instruction := solana.NewInstruction(
		programID,
		solana.AccountMetaSlice{
			{PublicKey: signerPub, IsSigner: true, IsWritable: true},
		},
		[]byte("xyz"),
	)

	ctx := t.Context()
	opts := &telemetry.ExecuteTransactionOptions{}
	sig, res, err := exec.ExecuteTransaction(ctx, instruction, opts)

	require.ErrorContains(t, err, "transaction not found or missing metadata")
	require.Equal(t, solana.Signature{}, sig)
	require.Nil(t, res)
}
