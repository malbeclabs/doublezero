package geolocation_test

import (
	"strings"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/geolocation"
	"github.com/stretchr/testify/require"
)

func TestSDK_Geolocation_DeriveProgramConfigPDA(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	pda1, bump1, err := geolocation.DeriveProgramConfigPDA(programID)
	require.NoError(t, err)
	require.False(t, pda1.IsZero(), "PDA should not be zero")
	require.LessOrEqual(t, int(bump1), 255, "Invalid bump seed")

	// Same inputs produce same PDA (determinism)
	pda2, bump2, err := geolocation.DeriveProgramConfigPDA(programID)
	require.NoError(t, err)
	require.Equal(t, pda1, pda2, "PDA should be deterministic")
	require.Equal(t, bump1, bump2, "Bump should be deterministic")
}

func TestSDK_Geolocation_DeriveProgramConfigPDA_DifferentPrograms(t *testing.T) {
	t.Parallel()

	programID1 := solana.NewWallet().PublicKey()
	programID2 := solana.NewWallet().PublicKey()

	pda1, _, err := geolocation.DeriveProgramConfigPDA(programID1)
	require.NoError(t, err)

	pda2, _, err := geolocation.DeriveProgramConfigPDA(programID2)
	require.NoError(t, err)

	require.NotEqual(t, pda1, pda2, "PDAs should be different for different program IDs")
}

func TestSDK_Geolocation_DeriveGeoProbePDA(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	code := "ams-probe-01"

	pda1, bump1, err := geolocation.DeriveGeoProbePDA(programID, code)
	require.NoError(t, err)
	require.False(t, pda1.IsZero(), "PDA should not be zero")
	require.LessOrEqual(t, int(bump1), 255, "Invalid bump seed")

	// Same inputs produce same PDA (determinism)
	pda2, bump2, err := geolocation.DeriveGeoProbePDA(programID, code)
	require.NoError(t, err)
	require.Equal(t, pda1, pda2, "PDA should be deterministic")
	require.Equal(t, bump1, bump2, "Bump should be deterministic")
}

func TestSDK_Geolocation_DeriveGeoProbePDA_DifferentCodes(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	pda1, _, err := geolocation.DeriveGeoProbePDA(programID, "ams-probe-01")
	require.NoError(t, err)

	pda2, _, err := geolocation.DeriveGeoProbePDA(programID, "fra-probe-01")
	require.NoError(t, err)

	require.NotEqual(t, pda1, pda2, "PDAs should be different for different codes")
}

func TestSDK_Geolocation_DeriveGeoProbePDA_DifferentPrograms(t *testing.T) {
	t.Parallel()

	programID1 := solana.NewWallet().PublicKey()
	programID2 := solana.NewWallet().PublicKey()
	code := "ams-probe-01"

	pda1, _, err := geolocation.DeriveGeoProbePDA(programID1, code)
	require.NoError(t, err)

	pda2, _, err := geolocation.DeriveGeoProbePDA(programID2, code)
	require.NoError(t, err)

	require.NotEqual(t, pda1, pda2, "PDAs should be different for different program IDs")
}

func TestSDK_Geolocation_DeriveGeoProbePDA_EmptyCode(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	_, _, err := geolocation.DeriveGeoProbePDA(programID, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "code is required")
}

func TestSDK_Geolocation_DeriveGeoProbePDA_CodeTooLong(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	longCode := strings.Repeat("a", geolocation.MaxCodeLength+1)
	_, _, err := geolocation.DeriveGeoProbePDA(programID, longCode)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max")
}
