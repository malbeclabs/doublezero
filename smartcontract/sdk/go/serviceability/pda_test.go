package serviceability_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PDAs are deterministic from (program_id, seeds), so we can cross-check the new
// helpers against an independent recomputation that mirrors the Rust seed bytes
// exactly. These tests catch typos in seed strings and width/endianness mistakes
// in the index encoding without requiring the Rust binary at test time.

func recomputePDA(t *testing.T, programID solana.PublicKey, seeds [][]byte) solana.PublicKey {
	t.Helper()
	pda, _, err := solana.FindProgramAddress(seeds, programID)
	require.NoError(t, err)
	return pda
}

func TestGetUserPDA_MatchesRustSeeds(t *testing.T) {
	t.Parallel()
	programID := solana.NewWallet().PublicKey()
	ip := [4]byte{198, 51, 100, 7}

	got, _, err := serviceability.GetUserPDA(programID, ip, serviceability.UserTypeIBRLWithAllocatedIP)
	require.NoError(t, err)

	want := recomputePDA(t, programID, [][]byte{
		[]byte("doublezero"),
		[]byte("user"),
		ip[:],
		{byte(serviceability.UserTypeIBRLWithAllocatedIP)},
	})
	assert.Equal(t, want, got)
}

func TestGetAccessPassPDA_MatchesRustSeeds(t *testing.T) {
	t.Parallel()
	programID := solana.NewWallet().PublicKey()
	userPayer := solana.NewWallet().PublicKey()
	ip := [4]byte{10, 0, 0, 5}

	got, _, err := serviceability.GetAccessPassPDA(programID, ip, userPayer)
	require.NoError(t, err)

	want := recomputePDA(t, programID, [][]byte{
		[]byte("doublezero"),
		[]byte("accesspass"),
		ip[:],
		userPayer[:],
	})
	assert.Equal(t, want, got)
}

func TestGetTunnelIdsPDA_IndexIsEightByteLE(t *testing.T) {
	t.Parallel()
	programID := solana.NewWallet().PublicKey()
	device := solana.NewWallet().PublicKey()

	for _, idx := range []uint64{0, 1, 7, 256, 0xDEAD_BEEF} {
		got, _, err := serviceability.GetTunnelIdsPDA(programID, device, idx)
		require.NoError(t, err)

		// Build the index seed by hand: 8-byte little-endian.
		idxBytes := []byte{
			byte(idx), byte(idx >> 8), byte(idx >> 16), byte(idx >> 24),
			byte(idx >> 32), byte(idx >> 40), byte(idx >> 48), byte(idx >> 56),
		}
		want := recomputePDA(t, programID, [][]byte{
			[]byte("doublezero"),
			[]byte("tunnelids"),
			device[:],
			idxBytes,
		})
		assert.Equal(t, want, got, "idx=%d", idx)
	}
}

func TestGetDzPrefixBlockPDA_IndexIsEightByteLE(t *testing.T) {
	t.Parallel()
	programID := solana.NewWallet().PublicKey()
	device := solana.NewWallet().PublicKey()

	idx := uint64(3)
	got, _, err := serviceability.GetDzPrefixBlockPDA(programID, device, idx)
	require.NoError(t, err)
	want := recomputePDA(t, programID, [][]byte{
		[]byte("doublezero"),
		[]byte("dzprefixblock"),
		device[:],
		{0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	})
	assert.Equal(t, want, got)
}

// TestUserPDA_DiffersByUserType guards against accidentally dropping the
// user_type byte from the seeds (which would collapse different user types onto
// the same PDA).
func TestUserPDA_DiffersByUserType(t *testing.T) {
	t.Parallel()
	programID := solana.NewWallet().PublicKey()
	ip := [4]byte{10, 0, 0, 7}

	pdaIBRL, _, err := serviceability.GetUserPDA(programID, ip, serviceability.UserTypeIBRL)
	require.NoError(t, err)
	pdaMulticast, _, err := serviceability.GetUserPDA(programID, ip, serviceability.UserTypeMulticast)
	require.NoError(t, err)
	assert.NotEqual(t, pdaIBRL, pdaMulticast)
}
