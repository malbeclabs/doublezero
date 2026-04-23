package geolocation_test

import (
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Geolocation_Instructions_AddTarget_Serialization(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	probePK := solana.NewWallet().PublicKey()
	targetPK := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildAddTargetInstruction(programID, signerPK, geolocation.AddTargetInstructionConfig{
		Code:               "test-user",
		ProbePK:            probePK,
		TargetType:         geolocation.GeoLocationTargetTypeOutbound,
		IPAddress:          [4]uint8{8, 8, 8, 8},
		LocationOffsetPort: 12345,
		TargetPK:           targetPK,
	})
	require.NoError(t, err)

	data, err := ix.Data()
	require.NoError(t, err)

	// Byte 0: discriminator (10).
	require.Equal(t, uint8(10), data[0], "discriminator should be AddTarget (10)")

	// Byte 1: target type (0 = Outbound).
	require.Equal(t, uint8(0), data[1])

	// Bytes 2-5: IP address.
	require.Equal(t, [4]byte{8, 8, 8, 8}, [4]byte(data[2:6]))

	// Bytes 6-7: LocationOffsetPort (LE).
	port := binary.LittleEndian.Uint16(data[6:8])
	require.Equal(t, uint16(12345), port)

	// Bytes 8-39: TargetPK.
	var gotTargetPK [32]byte
	copy(gotTargetPK[:], data[8:40])
	require.Equal(t, [32]byte(targetPK), gotTargetPK)
}

func TestSDK_Geolocation_Instructions_RemoveTarget_Serialization(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	probePK := solana.NewWallet().PublicKey()
	targetPK := solana.NewWallet().PublicKey()
	serviceabilityGS := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildRemoveTargetInstruction(programID, signerPK, geolocation.RemoveTargetInstructionConfig{
		Code:                      "test-user",
		ProbePK:                   probePK,
		TargetType:                geolocation.GeoLocationTargetTypeInbound,
		IPAddress:                 [4]uint8{1, 2, 3, 4},
		TargetPK:                  targetPK,
		ServiceabilityGlobalState: serviceabilityGS,
	})
	require.NoError(t, err)

	data, err := ix.Data()
	require.NoError(t, err)

	// Byte 0: discriminator (11).
	require.Equal(t, uint8(11), data[0], "discriminator should be RemoveTarget (11)")

	// Byte 1: target type (1 = Inbound).
	require.Equal(t, uint8(1), data[1])

	// Bytes 2-5: IP address.
	require.Equal(t, [4]byte{1, 2, 3, 4}, [4]byte(data[2:6]))

	// Bytes 6-37: TargetPK.
	var gotTargetPK [32]byte
	copy(gotTargetPK[:], data[6:38])
	require.Equal(t, [32]byte(targetPK), gotTargetPK)
}

func TestSDK_Geolocation_Instructions_AllDiscriminators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		index    int
		expected uint8
	}{
		{"AddTarget", geolocation.AddTargetInstructionIndex, 10},
		{"RemoveTarget", geolocation.RemoveTargetInstructionIndex, 11},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, int(tt.expected), tt.index)
		})
	}
}
