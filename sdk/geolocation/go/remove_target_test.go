package geolocation_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/require"
)

func TestBuildRemoveTargetInstruction_Valid(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	probePK := solana.NewWallet().PublicKey()
	targetPK := solana.NewWallet().PublicKey()
	serviceabilityGS := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildRemoveTargetInstruction(programID, signerPK, geolocation.RemoveTargetInstructionConfig{
		Code:                      "test-user",
		ProbePK:                   probePK,
		TargetType:                geolocation.GeoLocationTargetTypeOutbound,
		IPAddress:                 [4]uint8{8, 8, 4, 4},
		TargetPK:                  targetPK,
		ServiceabilityGlobalState: serviceabilityGS,
	})
	require.NoError(t, err)
	require.NotNil(t, ix)

	// Verify program ID.
	require.Equal(t, programID, ix.ProgramID())

	// Verify accounts: user_pda, probe_pk, config_pda, serviceability_gs, signer, system_program.
	accounts := ix.Accounts()
	require.Len(t, accounts, 6, "expected 6 accounts: user_pda, probe_pk, config_pda, serviceability_gs, signer, system_program")

	// Derive expected PDAs.
	expectedUserPDA, _, err := geolocation.DeriveGeolocationUserPDA(programID, "test-user")
	require.NoError(t, err)
	expectedConfigPDA, _, err := geolocation.DeriveProgramConfigPDA(programID)
	require.NoError(t, err)

	// Account 0: user PDA (writable, not signer).
	require.Equal(t, expectedUserPDA, accounts[0].PublicKey)
	require.True(t, accounts[0].IsWritable)
	require.False(t, accounts[0].IsSigner)

	// Account 1: probe PK (writable, not signer).
	require.Equal(t, probePK, accounts[1].PublicKey)
	require.True(t, accounts[1].IsWritable)
	require.False(t, accounts[1].IsSigner)

	// Account 2: config PDA (not writable, not signer).
	require.Equal(t, expectedConfigPDA, accounts[2].PublicKey)
	require.False(t, accounts[2].IsWritable)
	require.False(t, accounts[2].IsSigner)

	// Account 3: serviceability global state (not writable, not signer).
	require.Equal(t, serviceabilityGS, accounts[3].PublicKey)
	require.False(t, accounts[3].IsWritable)
	require.False(t, accounts[3].IsSigner)

	// Account 4: signer (writable, signer).
	require.Equal(t, signerPK, accounts[4].PublicKey)
	require.True(t, accounts[4].IsWritable)
	require.True(t, accounts[4].IsSigner)

	// Account 5: system program (not writable, not signer).
	require.Equal(t, solana.SystemProgramID, accounts[5].PublicKey)
	require.False(t, accounts[5].IsWritable)
	require.False(t, accounts[5].IsSigner)
}

func TestBuildRemoveTargetInstruction_EmptyCode(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()

	_, err := geolocation.BuildRemoveTargetInstruction(programID, signerPK, geolocation.RemoveTargetInstructionConfig{
		Code:                      "",
		ProbePK:                   solana.NewWallet().PublicKey(),
		TargetType:                geolocation.GeoLocationTargetTypeOutbound,
		IPAddress:                 [4]uint8{8, 8, 8, 8},
		TargetPK:                  solana.NewWallet().PublicKey(),
		ServiceabilityGlobalState: solana.NewWallet().PublicKey(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "code is required")
}

func TestBuildRemoveTargetInstruction_ZeroProbePK(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()

	_, err := geolocation.BuildRemoveTargetInstruction(programID, signerPK, geolocation.RemoveTargetInstructionConfig{
		Code:                      "test-user",
		ProbePK:                   solana.PublicKey{},
		TargetType:                geolocation.GeoLocationTargetTypeOutbound,
		IPAddress:                 [4]uint8{8, 8, 8, 8},
		TargetPK:                  solana.NewWallet().PublicKey(),
		ServiceabilityGlobalState: solana.NewWallet().PublicKey(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "probe public key is required")
}
