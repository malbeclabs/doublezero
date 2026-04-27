package geolocation_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/require"
)

func TestNewExecutor(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	executor := geolocation.NewExecutor(slog.Default(), nil, &signer, programID)
	require.NotNil(t, executor, "executor should not be nil")
}

func TestNewExecutor_WithTimeout(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	customTimeout := 10 * time.Second
	executor := geolocation.NewExecutor(slog.Default(), nil, &signer, programID, geolocation.WithWaitForVisibleTimeout(customTimeout))
	require.NotNil(t, executor, "executor should not be nil with custom timeout")
}

func TestExecuteTransaction_NoPrivateKey(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	executor := geolocation.NewExecutor(slog.Default(), nil, nil, programID)

	dummyIxs := dummyInstructionsFor(t, programID, solana.NewWallet().PublicKey())

	_, _, err := executor.ExecuteTransactions(context.Background(), dummyIxs, nil)
	require.ErrorIs(t, err, geolocation.ErrNoPrivateKey)
}

func TestExecuteTransaction_NoProgramID(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	zeroProgramID := solana.PublicKey{} // zero value

	executor := geolocation.NewExecutor(slog.Default(), nil, &signer, zeroProgramID)

	// Build dummy instructions using a non-zero program ID (the builder needs it to derive PDAs).
	// The executor checks its own programID field, not the instruction's.
	validProgramID := solana.NewWallet().PublicKey()
	dummyIxs := dummyInstructionsFor(t, validProgramID, solana.NewWallet().PublicKey())

	_, _, err := executor.ExecuteTransactions(context.Background(), dummyIxs, nil)
	require.ErrorIs(t, err, geolocation.ErrNoProgramID)
}

// dummyInstructionsFor returns a valid AddTarget instruction list whose signer
// matches the given wallet — sufficient for tx.Sign to succeed inside ExecuteTransactions.
func dummyInstructionsFor(t *testing.T, programID solana.PublicKey, signerPK solana.PublicKey) []solana.Instruction {
	t.Helper()
	ixs, err := geolocation.BuildAddTargetInstructions(programID, signerPK, geolocation.AddTargetInstructionConfig{
		Code:               "test-user",
		ProbePK:            solana.NewWallet().PublicKey(),
		TargetType:         geolocation.GeoLocationTargetTypeOutbound,
		IPAddress:          [4]uint8{8, 8, 8, 8},
		LocationOffsetPort: 443,
	})
	require.NoError(t, err)
	return ixs
}

func finalizedStatusResult() *solanarpc.GetSignatureStatusesResult {
	return &solanarpc.GetSignatureStatusesResult{
		Value: []*solanarpc.SignatureStatusesResult{
			{ConfirmationStatus: solanarpc.ConfirmationStatusFinalized},
		},
	}
}

func finalizedTxResult() *solanarpc.GetTransactionResult {
	return &solanarpc.GetTransactionResult{Meta: &solanarpc.TransactionMeta{}}
}

func TestExecuteTransaction_HappyPath(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	sig := solana.Signature{1, 2, 3}

	rpc := &mockExecutorRPCClient{
		GetLatestBlockhashFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: solana.Hash{9}},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
			return sig, nil
		},
		GetSignatureStatusesFunc: func(context.Context, bool, ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return finalizedStatusResult(), nil
		},
		GetTransactionFunc: func(context.Context, solana.Signature, *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return finalizedTxResult(), nil
		},
	}

	e := geolocation.NewExecutor(slog.Default(), rpc, &signer, programID)
	ixs := dummyInstructionsFor(t, programID, signer.PublicKey())

	gotSig, res, err := e.ExecuteTransactions(context.Background(), ixs, nil)
	require.NoError(t, err)
	require.Equal(t, sig, gotSig)
	require.NotNil(t, res)
}

func TestExecuteTransaction_BlockhashError(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	wantErr := errors.New("blockhash unavailable")

	rpc := &mockExecutorRPCClient{
		GetLatestBlockhashFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return nil, wantErr
		},
	}
	e := geolocation.NewExecutor(slog.Default(), rpc, &signer, programID)
	ixs := dummyInstructionsFor(t, programID, signer.PublicKey())

	_, _, err := e.ExecuteTransactions(context.Background(), ixs, nil)
	require.ErrorIs(t, err, wantErr)
}

func TestExecuteTransaction_SendError(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	wantErr := errors.New("send failed")

	rpc := &mockExecutorRPCClient{
		GetLatestBlockhashFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: solana.Hash{9}},
			}, nil
		},
		SendTransactionWithOptsFunc: func(context.Context, *solana.Transaction, solanarpc.TransactionOpts) (solana.Signature, error) {
			return solana.Signature{}, wantErr
		},
	}
	e := geolocation.NewExecutor(slog.Default(), rpc, &signer, programID)
	ixs := dummyInstructionsFor(t, programID, signer.PublicKey())

	_, _, err := e.ExecuteTransactions(context.Background(), ixs, nil)
	require.ErrorIs(t, err, wantErr)
}

func TestExecuteTransaction_ConfirmationTransitions(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	sig := solana.Signature{7}

	// Return Processed, then Confirmed, then Finalized on successive calls.
	var call atomic.Int32
	rpc := &mockExecutorRPCClient{
		GetLatestBlockhashFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: solana.Hash{9}},
			}, nil
		},
		SendTransactionWithOptsFunc: func(context.Context, *solana.Transaction, solanarpc.TransactionOpts) (solana.Signature, error) {
			return sig, nil
		},
		GetSignatureStatusesFunc: func(context.Context, bool, ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			var status solanarpc.ConfirmationStatusType
			switch call.Add(1) {
			case 1:
				status = solanarpc.ConfirmationStatusProcessed
			case 2:
				status = solanarpc.ConfirmationStatusConfirmed
			default:
				status = solanarpc.ConfirmationStatusFinalized
			}
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{ConfirmationStatus: status},
				},
			}, nil
		},
		GetTransactionFunc: func(context.Context, solana.Signature, *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return finalizedTxResult(), nil
		},
	}

	e := geolocation.NewExecutor(slog.Default(), rpc, &signer, programID)
	ixs := dummyInstructionsFor(t, programID, signer.PublicKey())

	_, _, err := e.ExecuteTransactions(context.Background(), ixs, nil)
	require.NoError(t, err)
	require.GreaterOrEqual(t, call.Load(), int32(3), "should have polled through Processed/Confirmed before Finalized")
}

func TestExecuteTransaction_WaitForVisibleTimeout(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	sig := solana.Signature{5}

	rpc := &mockExecutorRPCClient{
		GetLatestBlockhashFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: solana.Hash{9}},
			}, nil
		},
		SendTransactionWithOptsFunc: func(context.Context, *solana.Transaction, solanarpc.TransactionOpts) (solana.Signature, error) {
			return sig, nil
		},
		GetSignatureStatusesFunc: func(context.Context, bool, ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{Value: []*solanarpc.SignatureStatusesResult{nil}}, nil
		},
	}

	e := geolocation.NewExecutor(slog.Default(), rpc, &signer, programID,
		geolocation.WithWaitForVisibleTimeout(200*time.Millisecond),
	)
	ixs := dummyInstructionsFor(t, programID, signer.PublicKey())

	_, _, err := e.ExecuteTransactions(context.Background(), ixs, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not visible")
}

func TestExecuteTransaction_ContextCancellationDuringFinalization(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	sig := solana.Signature{6}

	// Keep returning Confirmed (not Finalized) so the executor stays in the wait loop.
	rpc := &mockExecutorRPCClient{
		GetLatestBlockhashFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: solana.Hash{9}},
			}, nil
		},
		SendTransactionWithOptsFunc: func(context.Context, *solana.Transaction, solanarpc.TransactionOpts) (solana.Signature, error) {
			return sig, nil
		},
		GetSignatureStatusesFunc: func(context.Context, bool, ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{ConfirmationStatus: solanarpc.ConfirmationStatusConfirmed},
				},
			}, nil
		},
	}

	e := geolocation.NewExecutor(slog.Default(), rpc, &signer, programID)
	ixs := dummyInstructionsFor(t, programID, signer.PublicKey())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	_, _, err := e.ExecuteTransactions(ctx, ixs, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestExecuteTransaction_FinalizationTimeout(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	sig := solana.Signature{8}

	rpc := &mockExecutorRPCClient{
		GetLatestBlockhashFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: solana.Hash{9}},
			}, nil
		},
		SendTransactionWithOptsFunc: func(context.Context, *solana.Transaction, solanarpc.TransactionOpts) (solana.Signature, error) {
			return sig, nil
		},
		GetSignatureStatusesFunc: func(context.Context, bool, ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return &solanarpc.GetSignatureStatusesResult{
				Value: []*solanarpc.SignatureStatusesResult{
					{ConfirmationStatus: solanarpc.ConfirmationStatusProcessed},
				},
			}, nil
		},
	}

	e := geolocation.NewExecutor(slog.Default(), rpc, &signer, programID,
		geolocation.WithFinalizationTimeout(500*time.Millisecond),
	)
	ixs := dummyInstructionsFor(t, programID, signer.PublicKey())

	_, _, err := e.ExecuteTransactions(context.Background(), ixs, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not finalized within")
}

func TestExecuteTransaction_SkipPreflightPassthrough(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	sig := solana.Signature{4}

	var observed atomic.Bool
	rpc := &mockExecutorRPCClient{
		GetLatestBlockhashFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
			return &solanarpc.GetLatestBlockhashResult{
				Value: &solanarpc.LatestBlockhashResult{Blockhash: solana.Hash{9}},
			}, nil
		},
		SendTransactionWithOptsFunc: func(_ context.Context, _ *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
			observed.Store(opts.SkipPreflight)
			return sig, nil
		},
		GetSignatureStatusesFunc: func(context.Context, bool, ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
			return finalizedStatusResult(), nil
		},
		GetTransactionFunc: func(context.Context, solana.Signature, *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
			return finalizedTxResult(), nil
		},
	}

	e := geolocation.NewExecutor(slog.Default(), rpc, &signer, programID)
	ixs := dummyInstructionsFor(t, programID, signer.PublicKey())

	_, _, err := e.ExecuteTransactions(context.Background(), ixs, &geolocation.ExecuteTransactionOptions{SkipPreflight: true})
	require.NoError(t, err)
	require.True(t, observed.Load(), "mock should have received SkipPreflight=true")
}
