package geolocation_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Geolocation_Instructions_CreateGeolocationUser_Serialization(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	tokenAccount := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildCreateGeolocationUserInstruction(programID, signerPK, geolocation.CreateGeolocationUserInstructionConfig{
		Code:         "test-user",
		TokenAccount: tokenAccount,
	})
	require.NoError(t, err)

	data, err := ix.Data()
	require.NoError(t, err)

	// First byte is discriminator (7).
	require.Equal(t, uint8(7), data[0], "discriminator should be CreateGeolocationUser (7)")

	// Next comes the Borsh string: 4-byte LE length prefix + UTF-8 bytes.
	codeLen := binary.LittleEndian.Uint32(data[1:5])
	require.Equal(t, uint32(len("test-user")), codeLen)
	require.Equal(t, "test-user", string(data[5:5+codeLen]))

	// After the code string: 32-byte token account public key.
	offset := 5 + codeLen
	var tokenAccountBytes [32]byte
	copy(tokenAccountBytes[:], data[offset:offset+32])
	require.Equal(t, [32]byte(tokenAccount), tokenAccountBytes)
}

func TestSDK_Geolocation_Instructions_UpdateGeolocationUser_Serialization(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	tokenAccount := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildUpdateGeolocationUserInstruction(programID, signerPK, geolocation.UpdateGeolocationUserInstructionConfig{
		Code:         "test-user",
		TokenAccount: &tokenAccount,
	})
	require.NoError(t, err)

	data, err := ix.Data()
	require.NoError(t, err)

	// First byte is discriminator (8).
	require.Equal(t, uint8(8), data[0], "discriminator should be UpdateGeolocationUser (8)")

	// Next: Borsh Option<[32]byte> — 1 byte for Some(1) + 32-byte pubkey.
	require.Equal(t, uint8(1), data[1], "Option should be Some (1)")
	var tokenAccountBytes [32]byte
	copy(tokenAccountBytes[:], data[2:34])
	require.Equal(t, [32]byte(tokenAccount), tokenAccountBytes)
}

func TestSDK_Geolocation_Instructions_DeleteGeolocationUser_Serialization(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	serviceabilityGS := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildDeleteGeolocationUserInstruction(programID, signerPK, geolocation.DeleteGeolocationUserInstructionConfig{
		Code:                      "test-user",
		ServiceabilityGlobalState: serviceabilityGS,
	})
	require.NoError(t, err)

	data, err := ix.Data()
	require.NoError(t, err)

	// Only the discriminator byte (9).
	require.Len(t, data, 1)
	require.Equal(t, uint8(9), data[0], "discriminator should be DeleteGeolocationUser (9)")
}

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

func TestSDK_Geolocation_Instructions_SetResultDestination_Serialization(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildSetResultDestinationInstruction(programID, signerPK, geolocation.SetResultDestinationInstructionConfig{
		Code:        "test-user",
		Destination: "https://example.com/results",
	})
	require.NoError(t, err)

	data, err := ix.Data()
	require.NoError(t, err)

	// Byte 0: discriminator (13).
	require.Equal(t, uint8(13), data[0], "discriminator should be SetResultDestination (13)")

	// Next: Borsh string: 4-byte LE length prefix + UTF-8 bytes.
	destLen := binary.LittleEndian.Uint32(data[1:5])
	require.Equal(t, uint32(len("https://example.com/results")), destLen)
	require.Equal(t, "https://example.com/results", string(data[5:5+destLen]))
}

func TestSDK_Geolocation_Instructions_SetResultDestination_Clear_Serialization(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()

	ix, err := geolocation.BuildSetResultDestinationInstruction(programID, signerPK, geolocation.SetResultDestinationInstructionConfig{
		Code:        "test-user",
		Destination: "",
	})
	require.NoError(t, err)

	data, err := ix.Data()
	require.NoError(t, err)

	// Byte 0: discriminator (13).
	require.Equal(t, uint8(13), data[0])

	// Empty string: 4-byte LE zero length.
	destLen := binary.LittleEndian.Uint32(data[1:5])
	require.Equal(t, uint32(0), destLen)
	require.Len(t, data, 5, "no string bytes after zero-length prefix")
}

func TestSDK_Geolocation_Instructions_AllDiscriminators(t *testing.T) {
	t.Parallel()

	// Verify discriminator constants match expected values.
	tests := []struct {
		name     string
		index    int
		expected uint8
	}{
		{"CreateGeolocationUser", geolocation.CreateGeolocationUserInstructionIndex, 7},
		{"UpdateGeolocationUser", geolocation.UpdateGeolocationUserInstructionIndex, 8},
		{"DeleteGeolocationUser", geolocation.DeleteGeolocationUserInstructionIndex, 9},
		{"AddTarget", geolocation.AddTargetInstructionIndex, 10},
		{"RemoveTarget", geolocation.RemoveTargetInstructionIndex, 11},
		{"SetResultDestination", geolocation.SetResultDestinationInstructionIndex, 13},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, int(tt.expected), tt.index)
		})
	}
}

func TestSDK_Geolocation_Instructions_CreateUser_CodeTooLong_NoSerialization(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	longCode := strings.Repeat("x", geolocation.MaxCodeLength+1)

	_, err := geolocation.BuildCreateGeolocationUserInstruction(programID, signerPK, geolocation.CreateGeolocationUserInstructionConfig{
		Code:         longCode,
		TokenAccount: solana.NewWallet().PublicKey(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max")
}
