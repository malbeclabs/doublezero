package geolocation_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
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

	// Build a dummy instruction to pass to ExecuteTransaction.
	dummyIx, err := geolocation.BuildAddTargetInstruction(programID, solana.NewWallet().PublicKey(), geolocation.AddTargetInstructionConfig{
		Code:               "test-user",
		ProbePK:            solana.NewWallet().PublicKey(),
		TargetType:         geolocation.GeoLocationTargetTypeOutbound,
		IPAddress:          [4]uint8{8, 8, 8, 8},
		LocationOffsetPort: 443,
	})
	require.NoError(t, err)

	_, _, err = executor.ExecuteTransaction(context.Background(), dummyIx, nil)
	require.ErrorIs(t, err, geolocation.ErrNoPrivateKey)
}

func TestExecuteTransaction_NoProgramID(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	zeroProgramID := solana.PublicKey{} // zero value

	executor := geolocation.NewExecutor(slog.Default(), nil, &signer, zeroProgramID)

	// Build a dummy instruction using a non-zero program ID (the builder needs it to derive PDAs).
	// The executor checks its own programID field, not the instruction's.
	validProgramID := solana.NewWallet().PublicKey()
	dummyIx, err := geolocation.BuildAddTargetInstruction(validProgramID, solana.NewWallet().PublicKey(), geolocation.AddTargetInstructionConfig{
		Code:               "test-user",
		ProbePK:            solana.NewWallet().PublicKey(),
		TargetType:         geolocation.GeoLocationTargetTypeOutbound,
		IPAddress:          [4]uint8{8, 8, 8, 8},
		LocationOffsetPort: 443,
	})
	require.NoError(t, err)

	_, _, err = executor.ExecuteTransaction(context.Background(), dummyIx, nil)
	require.ErrorIs(t, err, geolocation.ErrNoProgramID)
}
