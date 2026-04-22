package geolocation_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/require"
)

func TestBuildSetResultDestinationInstruction_Set(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	probePK1 := solana.NewWallet().PublicKey()
	probePK2 := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildSetResultDestinationInstruction(programID, signerPK, geolocation.SetResultDestinationInstructionConfig{
		Code:        "test-user",
		Destination: "https://example.com/results",
		ProbePKs:    []solana.PublicKey{probePK1, probePK2},
	})
	require.NoError(t, err)
	require.NotNil(t, ix)

	// Verify program ID.
	require.Equal(t, programID, ix.ProgramID())

	// Verify accounts: user_pda, probe1, probe2, signer, system_program.
	accounts := ix.Accounts()
	require.Len(t, accounts, 5, "expected 5 accounts: user_pda + 2 probes + signer + system_program")

	// Derive expected user PDA.
	expectedUserPDA, _, err := geolocation.DeriveGeolocationUserPDA(programID, "test-user")
	require.NoError(t, err)

	// Account 0: user PDA (writable, not signer).
	require.Equal(t, expectedUserPDA, accounts[0].PublicKey)
	require.True(t, accounts[0].IsWritable)
	require.False(t, accounts[0].IsSigner)

	// Account 1: probe PK 1 (writable, not signer).
	require.Equal(t, probePK1, accounts[1].PublicKey)
	require.True(t, accounts[1].IsWritable)
	require.False(t, accounts[1].IsSigner)

	// Account 2: probe PK 2 (writable, not signer).
	require.Equal(t, probePK2, accounts[2].PublicKey)
	require.True(t, accounts[2].IsWritable)
	require.False(t, accounts[2].IsSigner)

	// Account 3: signer (writable, signer).
	require.Equal(t, signerPK, accounts[3].PublicKey)
	require.True(t, accounts[3].IsWritable)
	require.True(t, accounts[3].IsSigner)

	// Account 4: system program (not writable, not signer).
	require.Equal(t, solana.SystemProgramID, accounts[4].PublicKey)
	require.False(t, accounts[4].IsWritable)
	require.False(t, accounts[4].IsSigner)
}

func TestBuildSetResultDestinationInstruction_Clear(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildSetResultDestinationInstruction(programID, signerPK, geolocation.SetResultDestinationInstructionConfig{
		Code:        "test-user",
		Destination: "",
		ProbePKs:    nil,
	})
	require.NoError(t, err)
	require.NotNil(t, ix)

	// Verify accounts: user_pda, signer, system_program (no probes).
	accounts := ix.Accounts()
	require.Len(t, accounts, 3, "expected 3 accounts: user_pda + signer + system_program")

	// Derive expected user PDA.
	expectedUserPDA, _, err := geolocation.DeriveGeolocationUserPDA(programID, "test-user")
	require.NoError(t, err)

	// Account 0: user PDA.
	require.Equal(t, expectedUserPDA, accounts[0].PublicKey)
	require.True(t, accounts[0].IsWritable)
	require.False(t, accounts[0].IsSigner)

	// Account 1: signer.
	require.Equal(t, signerPK, accounts[1].PublicKey)
	require.True(t, accounts[1].IsWritable)
	require.True(t, accounts[1].IsSigner)

	// Account 2: system program.
	require.Equal(t, solana.SystemProgramID, accounts[2].PublicKey)
	require.False(t, accounts[2].IsWritable)
	require.False(t, accounts[2].IsSigner)
}

func TestBuildSetResultDestinationInstruction_EmptyCode(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()

	_, err := geolocation.BuildSetResultDestinationInstruction(programID, signerPK, geolocation.SetResultDestinationInstructionConfig{
		Code:        "",
		Destination: "https://example.com",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "code is required")
}

func TestDeriveUniqueProbePKs_Empty(t *testing.T) {
	t.Parallel()

	pks := geolocation.DeriveUniqueProbePKs(nil)
	require.Empty(t, pks)

	pks = geolocation.DeriveUniqueProbePKs([]geolocation.GeolocationTarget{})
	require.Empty(t, pks)
}

func TestDeriveUniqueProbePKs_Unique(t *testing.T) {
	t.Parallel()

	p1 := solana.NewWallet().PublicKey()
	p2 := solana.NewWallet().PublicKey()
	p3 := solana.NewWallet().PublicKey()

	targets := []geolocation.GeolocationTarget{
		{GeoProbePK: p1},
		{GeoProbePK: p2},
		{GeoProbePK: p3},
	}
	require.Equal(t, []solana.PublicKey{p1, p2, p3}, geolocation.DeriveUniqueProbePKs(targets))
}

func TestDeriveUniqueProbePKs_DedupesPreservingFirstOccurrence(t *testing.T) {
	t.Parallel()

	p1 := solana.NewWallet().PublicKey()
	p2 := solana.NewWallet().PublicKey()

	targets := []geolocation.GeolocationTarget{
		{GeoProbePK: p1},
		{GeoProbePK: p2},
		{GeoProbePK: p1}, // duplicate
		{GeoProbePK: p2}, // duplicate
	}
	require.Equal(t, []solana.PublicKey{p1, p2}, geolocation.DeriveUniqueProbePKs(targets))
}

// validUserAccountBytes returns a serialized GeolocationUser suitable for
// GetAccountInfo mocking.
func validUserAccountBytes(t *testing.T, owner solana.PublicKey, code string, targets []geolocation.GeolocationTarget) []byte {
	t.Helper()
	user := geolocation.GeolocationUser{
		AccountType:   geolocation.AccountTypeGeolocationUser,
		Owner:         owner,
		Code:          code,
		TokenAccount:  solana.NewWallet().PublicKey(),
		PaymentStatus: geolocation.GeolocationPaymentStatusPaid,
		Billing: geolocation.GeolocationBillingConfig{
			Variant:      geolocation.BillingConfigFlatPerEpoch,
			FlatPerEpoch: geolocation.FlatPerEpochConfig{Rate: 1, LastDeductionDzEpoch: 1},
		},
		Status:            geolocation.GeolocationUserStatusActivated,
		Targets:           targets,
		ResultDestination: "",
	}
	var buf bytes.Buffer
	require.NoError(t, user.Serialize(&buf))
	return buf.Bytes()
}

func TestExecutorSetResultDestination_NoPrivateKey(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	executor := geolocation.NewExecutor(slog.Default(), nil, nil, programID)

	_, _, err := executor.SetResultDestination(context.Background(), "test-user", "8.8.8.8:9000", nil)
	require.ErrorIs(t, err, geolocation.ErrNoPrivateKey)
}

func TestExecutorSetResultDestination_NoProgramID(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	executor := geolocation.NewExecutor(slog.Default(), nil, &signer, solana.PublicKey{})

	_, _, err := executor.SetResultDestination(context.Background(), "test-user", "8.8.8.8:9000", nil)
	require.ErrorIs(t, err, geolocation.ErrNoProgramID)
}

func TestExecutorSetResultDestination_EmptyCode(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	executor := geolocation.NewExecutor(slog.Default(), nil, &signer, programID)

	_, _, err := executor.SetResultDestination(context.Background(), "", "8.8.8.8:9000", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "code is required")
}

func TestExecutorSetResultDestination_AccountNotFound(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	rpc := &mockExecutorRPCClient{
		GetAccountInfoFunc: func(_ context.Context, _ solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			return &solanarpc.GetAccountInfoResult{Value: nil}, nil
		},
	}
	executor := geolocation.NewExecutor(slog.Default(), rpc, &signer, programID)

	_, _, err := executor.SetResultDestination(context.Background(), "test-user", "8.8.8.8:9000", nil)
	require.ErrorIs(t, err, geolocation.ErrAccountNotFound)
}

func TestExecutorSetResultDestination_OwnerMismatch(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	wrongOwner := solana.NewWallet().PublicKey()

	rpc := &mockExecutorRPCClient{
		GetAccountInfoFunc: func(_ context.Context, _ solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			data := validUserAccountBytes(t, signer.PublicKey(), "test-user", nil)
			return &solanarpc.GetAccountInfoResult{
				Value: &solanarpc.Account{
					Owner: wrongOwner,
					Data:  solanarpc.DataBytesOrJSONFromBytes(data),
				},
			}, nil
		},
	}
	executor := geolocation.NewExecutor(slog.Default(), rpc, &signer, programID)

	_, _, err := executor.SetResultDestination(context.Background(), "test-user", "8.8.8.8:9000", nil)
	require.ErrorIs(t, err, geolocation.ErrOwnerMismatch)
}
