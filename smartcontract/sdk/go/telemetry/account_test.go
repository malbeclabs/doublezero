package telemetry

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestDeriveDeviceLatencySamplesAccount(t *testing.T) {
	programID := solana.MustPublicKeyFromBase58("8x3X1VRUUqZ2UDs2xMo5V2d2Kk2yCNTe2TG7PsfE2uDw")
	agent := solana.MustPublicKeyFromBase58("2QtuSEANvdN9x6uFJZKdA45ZKq8YFvo1Ho4DP47vRLHP")
	origin := solana.MustPublicKeyFromBase58("DEvCe1kUN9TkfbHyi9RQ2cn8s4cDQFroC1Z9bbz6Hnm9")
	target := solana.MustPublicKeyFromBase58("DEvCe2q4C64rDfK6bZc6JYvXcysQHoekWEiZ1vT14uxW")
	link := solana.MustPublicKeyFromBase58("5EYw8N3K4nU6utduZu3d6C8yV2QQGHkEgr6n8XztRLMN")
	epoch := uint64(12345)

	seed, err := DeriveDeviceLatencySamplesSeed(programID, origin, target, link, epoch)
	require.NoError(t, err)
	require.Len(t, seed, 32)

	addr, err := solana.CreateWithSeed(agent, seed, programID)
	require.NoError(t, err)

	t.Logf("Seed: %s", seed)
	t.Logf("Derived address: %s", addr.String())

	// Match against expected values if known
	require.Equal(t, "9QgHDpkkJTDnFP1kSt5SmN3AnwZBQ4xo", seed) // fill in
	require.Equal(t, "AS8o3BVc9cptTcgV2ihBNfzwK3mYZbstnnB7gACcYL1e", addr.String())
}

func TestSDK_Telemetry_DeriveDeviceLatencySamplesAddress_IsDeterministic(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	agent := solana.NewWallet().PublicKey()
	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link := solana.NewWallet().PublicKey()
	epoch := uint64(100)

	addr1, seed1, err := DeriveDeviceLatencySamplesAddress(agent, programID, origin, target, link, epoch)
	require.NoError(t, err)
	require.False(t, addr1.IsZero(), "Derived address should not be zero")
	require.Len(t, seed1, 32, "Seed must be 32 characters")

	addr2, seed2, err := DeriveDeviceLatencySamplesAddress(agent, programID, origin, target, link, epoch)
	require.NoError(t, err)

	require.Equal(t, addr1, addr2, "Addresses should be deterministic")
	require.Equal(t, seed1, seed2, "Seeds should be deterministic")
}

func TestSDK_Telemetry_DeriveDeviceLatencySamplesAddress_VariesWithInputs(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	agent := solana.NewWallet().PublicKey()
	origin := solana.NewWallet().PublicKey()
	target := solana.NewWallet().PublicKey()
	link1 := solana.NewWallet().PublicKey()
	link2 := solana.NewWallet().PublicKey()
	epoch1 := uint64(100)
	epoch2 := uint64(101)

	addr1, _, err := DeriveDeviceLatencySamplesAddress(agent, programID, origin, target, link1, epoch1)
	require.NoError(t, err)

	addr2, _, err := DeriveDeviceLatencySamplesAddress(agent, programID, origin, target, link2, epoch1)
	require.NoError(t, err)

	addr3, _, err := DeriveDeviceLatencySamplesAddress(agent, programID, origin, target, link1, epoch2)
	require.NoError(t, err)

	require.NotEqual(t, addr1, addr2, "Addresses should differ for different links")
	require.NotEqual(t, addr1, addr3, "Addresses should differ for different epochs")
}
