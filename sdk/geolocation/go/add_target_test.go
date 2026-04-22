package geolocation_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/require"
)

func TestBuildAddTargetInstruction_Outbound(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	probePK := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildAddTargetInstruction(programID, signerPK, geolocation.AddTargetInstructionConfig{
		Code:               "test-user",
		ProbePK:            probePK,
		TargetType:         geolocation.GeoLocationTargetTypeOutbound,
		IPAddress:          [4]uint8{8, 8, 8, 8},
		LocationOffsetPort: 443,
		TargetPK:           solana.NewWallet().PublicKey(),
	})
	require.NoError(t, err)
	require.NotNil(t, ix)

	// Verify program ID.
	require.Equal(t, programID, ix.ProgramID())

	// Verify accounts: user_pda, probe_pk, signer, system_program.
	accounts := ix.Accounts()
	require.Len(t, accounts, 4, "expected 4 accounts: user_pda, probe_pk, signer, system_program")

	// Derive the expected user PDA.
	expectedUserPDA, _, err := geolocation.DeriveGeolocationUserPDA(programID, "test-user")
	require.NoError(t, err)

	// Account 0: user PDA (writable, not signer).
	require.Equal(t, expectedUserPDA, accounts[0].PublicKey)
	require.True(t, accounts[0].IsWritable)
	require.False(t, accounts[0].IsSigner)

	// Account 1: probe PK (writable, not signer).
	require.Equal(t, probePK, accounts[1].PublicKey)
	require.True(t, accounts[1].IsWritable)
	require.False(t, accounts[1].IsSigner)

	// Account 2: signer (writable, signer).
	require.Equal(t, signerPK, accounts[2].PublicKey)
	require.True(t, accounts[2].IsWritable)
	require.True(t, accounts[2].IsSigner)

	// Account 3: system program (not writable, not signer).
	require.Equal(t, solana.SystemProgramID, accounts[3].PublicKey)
	require.False(t, accounts[3].IsWritable)
	require.False(t, accounts[3].IsSigner)
}

func TestBuildAddTargetInstruction_Inbound(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	probePK := solana.NewWallet().PublicKey()
	targetPK := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildAddTargetInstruction(programID, signerPK, geolocation.AddTargetInstructionConfig{
		Code:               "test-user",
		ProbePK:            probePK,
		TargetType:         geolocation.GeoLocationTargetTypeInbound,
		IPAddress:          [4]uint8{1, 2, 3, 4},
		LocationOffsetPort: 8080,
		TargetPK:           targetPK,
	})
	require.NoError(t, err)
	require.NotNil(t, ix)

	// Verify the instruction data discriminator is AddTarget (10).
	data, err := ix.Data()
	require.NoError(t, err)
	require.Equal(t, uint8(10), data[0])

	// Verify inbound target type byte.
	require.Equal(t, uint8(1), data[1], "target type should be Inbound (1)")

	// Verify accounts have 4 entries.
	accounts := ix.Accounts()
	require.Len(t, accounts, 4)
}

func TestBuildAddTargetInstruction_OutboundIcmp(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	probePK := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildAddTargetInstruction(programID, signerPK, geolocation.AddTargetInstructionConfig{
		Code:               "test-user",
		ProbePK:            probePK,
		TargetType:         geolocation.GeoLocationTargetTypeOutboundIcmp,
		IPAddress:          [4]uint8{1, 1, 1, 1},
		LocationOffsetPort: 0,
		TargetPK:           solana.NewWallet().PublicKey(),
	})
	require.NoError(t, err)
	require.NotNil(t, ix)

	// Verify the instruction data discriminator is AddTarget (10).
	data, err := ix.Data()
	require.NoError(t, err)
	require.Equal(t, uint8(10), data[0])

	// Verify OutboundIcmp target type byte.
	require.Equal(t, uint8(2), data[1], "target type should be OutboundIcmp (2)")
}

func TestBuildAddTargetInstruction_PrivateIP(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	probePK := solana.NewWallet().PublicKey()

	tests := []struct {
		name string
		ip   [4]uint8
	}{
		{"10.x.x.x", [4]uint8{10, 0, 0, 1}},
		{"172.16.x.x", [4]uint8{172, 16, 0, 1}},
		{"192.168.x.x", [4]uint8{192, 168, 1, 1}},
		{"127.0.0.1", [4]uint8{127, 0, 0, 1}},
		{"0.0.0.0", [4]uint8{0, 0, 0, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := geolocation.BuildAddTargetInstruction(programID, signerPK, geolocation.AddTargetInstructionConfig{
				Code:               "test-user",
				ProbePK:            probePK,
				TargetType:         geolocation.GeoLocationTargetTypeOutbound,
				IPAddress:          tt.ip,
				LocationOffsetPort: 443,
				TargetPK:           solana.NewWallet().PublicKey(),
			})
			require.Error(t, err)
		})
	}
}

func TestBuildAddTargetInstruction_InboundDefaultTargetPK(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	probePK := solana.NewWallet().PublicKey()

	_, err := geolocation.BuildAddTargetInstruction(programID, signerPK, geolocation.AddTargetInstructionConfig{
		Code:               "test-user",
		ProbePK:            probePK,
		TargetType:         geolocation.GeoLocationTargetTypeInbound,
		IPAddress:          [4]uint8{1, 2, 3, 4},
		LocationOffsetPort: 8080,
		TargetPK:           solana.PublicKey{}, // zero value
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "target public key is required for inbound target type")
}
