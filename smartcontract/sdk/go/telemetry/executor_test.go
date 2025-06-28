package telemetry_test

import (
	"context"
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
