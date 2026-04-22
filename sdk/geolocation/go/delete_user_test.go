package geolocation_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/require"
)

func TestBuildDeleteGeolocationUserInstruction_Valid(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	serviceabilityGS := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildDeleteGeolocationUserInstruction(programID, signerPK, geolocation.DeleteGeolocationUserInstructionConfig{
		Code:                      "test-user",
		ServiceabilityGlobalState: serviceabilityGS,
	})
	require.NoError(t, err)
	require.NotNil(t, ix)

	// Verify program ID.
	require.Equal(t, programID, ix.ProgramID())

	// Verify accounts: user_pda, config_pda, serviceability_gs, signer, system_program.
	accounts := ix.Accounts()
	require.Len(t, accounts, 5, "expected 5 accounts: user_pda, config_pda, serviceability_gs, signer, system_program")

	// Derive expected PDAs.
	expectedUserPDA, _, err := geolocation.DeriveGeolocationUserPDA(programID, "test-user")
	require.NoError(t, err)
	expectedConfigPDA, _, err := geolocation.DeriveProgramConfigPDA(programID)
	require.NoError(t, err)

	// Account 0: user PDA (writable, not signer).
	require.Equal(t, expectedUserPDA, accounts[0].PublicKey)
	require.True(t, accounts[0].IsWritable)
	require.False(t, accounts[0].IsSigner)

	// Account 1: config PDA (not writable, not signer).
	require.Equal(t, expectedConfigPDA, accounts[1].PublicKey)
	require.False(t, accounts[1].IsWritable)
	require.False(t, accounts[1].IsSigner)

	// Account 2: serviceability global state (not writable, not signer).
	require.Equal(t, serviceabilityGS, accounts[2].PublicKey)
	require.False(t, accounts[2].IsWritable)
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

func TestBuildDeleteGeolocationUserInstruction_EmptyCode(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()

	_, err := geolocation.BuildDeleteGeolocationUserInstruction(programID, signerPK, geolocation.DeleteGeolocationUserInstructionConfig{
		Code:                      "",
		ServiceabilityGlobalState: solana.NewWallet().PublicKey(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "code is required")
}

func TestBuildDeleteGeolocationUserInstruction_ZeroServiceability(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()

	_, err := geolocation.BuildDeleteGeolocationUserInstruction(programID, signerPK, geolocation.DeleteGeolocationUserInstructionConfig{
		Code:                      "test-user",
		ServiceabilityGlobalState: solana.PublicKey{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "serviceability global state public key is required")
}
