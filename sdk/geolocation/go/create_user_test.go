package geolocation_test

import (
	"strings"
	"testing"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/require"
)

func TestBuildCreateGeolocationUserInstruction_Valid(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	tokenAccount := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildCreateGeolocationUserInstruction(programID, signerPK, geolocation.CreateGeolocationUserInstructionConfig{
		Code:         "test-user",
		TokenAccount: tokenAccount,
	})
	require.NoError(t, err)
	require.NotNil(t, ix)

	// Verify program ID.
	require.Equal(t, programID, ix.ProgramID())

	// Verify accounts: user_pda, signer, system_program.
	accounts := ix.Accounts()
	require.Len(t, accounts, 3, "expected 3 accounts: user_pda, signer, system_program")

	// Derive the expected user PDA.
	expectedUserPDA, _, err := geolocation.DeriveGeolocationUserPDA(programID, "test-user")
	require.NoError(t, err)

	// Account 0: user PDA (writable, not signer).
	require.Equal(t, expectedUserPDA, accounts[0].PublicKey)
	require.True(t, accounts[0].IsWritable, "user PDA should be writable")
	require.False(t, accounts[0].IsSigner, "user PDA should not be signer")

	// Account 1: signer (writable, signer).
	require.Equal(t, signerPK, accounts[1].PublicKey)
	require.True(t, accounts[1].IsWritable, "signer should be writable")
	require.True(t, accounts[1].IsSigner, "signer should be signer")

	// Account 2: system program (not writable, not signer).
	require.Equal(t, solana.SystemProgramID, accounts[2].PublicKey)
	require.False(t, accounts[2].IsWritable, "system program should not be writable")
	require.False(t, accounts[2].IsSigner, "system program should not be signer")
}

func TestBuildCreateGeolocationUserInstruction_EmptyCode(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()

	_, err := geolocation.BuildCreateGeolocationUserInstruction(programID, signerPK, geolocation.CreateGeolocationUserInstructionConfig{
		Code:         "",
		TokenAccount: solana.NewWallet().PublicKey(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "code is required")
}

func TestBuildCreateGeolocationUserInstruction_CodeTooLong(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	longCode := strings.Repeat("a", geolocation.MaxCodeLength+1)

	_, err := geolocation.BuildCreateGeolocationUserInstruction(programID, signerPK, geolocation.CreateGeolocationUserInstructionConfig{
		Code:         longCode,
		TokenAccount: solana.NewWallet().PublicKey(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max")
}
