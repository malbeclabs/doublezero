package geoprobe

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestSignOffset_Success(t *testing.T) {
	t.Parallel()

	// Generate a keypair for signing
	keypair := solana.NewWallet().PrivateKey
	signer := NewOffsetSigner(keypair)

	// Create an offset
	offset := &LocationOffset{
		MeasurementSlot: 12345,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   800000,
		RttNs:           800000,
		NumReferences:   0,
		References:      nil,
	}

	// Sign the offset
	err := signer.SignOffset(offset)
	require.NoError(t, err)

	// Verify signature is not empty
	require.NotEqual(t, [64]byte{}, offset.Signature)

	// Verify pubkey was set correctly
	expectedPubkey := keypair.PublicKey()
	require.Equal(t, expectedPubkey[:], offset.Pubkey[:])

	// Verify the signature is valid
	err = VerifyOffset(offset)
	require.NoError(t, err)
}

func TestVerifyOffset_InvalidSignature(t *testing.T) {
	t.Parallel()

	// Generate a keypair and sign an offset
	keypair := solana.NewWallet().PrivateKey
	signer := NewOffsetSigner(keypair)

	offset := &LocationOffset{
		MeasurementSlot: 99999,
		Lat:             50.1109,
		Lng:             8.6821,
		MeasuredRttNs:   1000000,
		RttNs:           1000000,
		NumReferences:   0,
		References:      nil,
	}

	err := signer.SignOffset(offset)
	require.NoError(t, err)

	// Tamper with the data (invalidates signature)
	TamperOffset(offset)

	// Verification should fail
	err = VerifyOffset(offset)
	require.Error(t, err)
	require.Contains(t, err.Error(), "signature verification failed")
}

func TestVerifyOffset_WrongPublicKey(t *testing.T) {
	t.Parallel()

	// Sign with one keypair
	keypair1 := solana.NewWallet().PrivateKey
	signer1 := NewOffsetSigner(keypair1)

	offset := &LocationOffset{
		MeasurementSlot: 55555,
		Lat:             1.0,
		Lng:             2.0,
		MeasuredRttNs:   500000,
		RttNs:           500000,
		NumReferences:   0,
		References:      nil,
	}

	err := signer1.SignOffset(offset)
	require.NoError(t, err)

	// Replace the public key with a different one
	keypair2 := solana.NewWallet().PrivateKey
	pubkey2 := keypair2.PublicKey()
	copy(offset.Pubkey[:], pubkey2[:])

	// Verification should fail (signature doesn't match new pubkey)
	err = VerifyOffset(offset)
	require.Error(t, err)
}

func TestSignOffset_WithReferences(t *testing.T) {
	t.Parallel()

	// Create DZD offset
	dzdKeypair := solana.NewWallet().PrivateKey
	dzdSigner := NewOffsetSigner(dzdKeypair)

	dzdOffset := &LocationOffset{
		MeasurementSlot: 100,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   800000,
		RttNs:           800000,
		NumReferences:   0,
		References:      nil,
	}

	err := dzdSigner.SignOffset(dzdOffset)
	require.NoError(t, err)

	// Create Probe offset that references DZD
	probeKeypair := solana.NewWallet().PrivateKey
	probeSigner := NewOffsetSigner(probeKeypair)

	probeOffset := &LocationOffset{
		MeasurementSlot: 101,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   12500000,
		RttNs:           13300000,
		NumReferences:   1,
		References:      []LocationOffset{*dzdOffset},
	}

	err = probeSigner.SignOffset(probeOffset)
	require.NoError(t, err)

	// Verify probe offset signature
	err = VerifyOffset(probeOffset)
	require.NoError(t, err)

	// Verify the reference is still valid
	err = VerifyOffset(&probeOffset.References[0])
	require.NoError(t, err)
}

func TestVerifyOffsetChain_Success(t *testing.T) {
	t.Parallel()

	// Create a 2-level chain: DZD -> Probe
	dzdKeypair := solana.NewWallet().PrivateKey
	dzdSigner := NewOffsetSigner(dzdKeypair)

	dzdOffset := &LocationOffset{
		MeasurementSlot: 100,
		Lat:             50.1109,
		Lng:             8.6821,
		MeasuredRttNs:   800000,
		RttNs:           800000,
		NumReferences:   0,
		References:      nil,
	}

	err := dzdSigner.SignOffset(dzdOffset)
	require.NoError(t, err)

	probeKeypair := solana.NewWallet().PrivateKey
	probeSigner := NewOffsetSigner(probeKeypair)

	probeOffset := &LocationOffset{
		MeasurementSlot: 101,
		Lat:             50.1109,
		Lng:             8.6821,
		MeasuredRttNs:   10000000,
		RttNs:           10800000,
		NumReferences:   1,
		References:      []LocationOffset{*dzdOffset},
	}

	err = probeSigner.SignOffset(probeOffset)
	require.NoError(t, err)

	// Verify the entire chain
	err = VerifyOffsetChain(probeOffset)
	require.NoError(t, err)
}

func TestVerifyOffsetChain_InvalidReference(t *testing.T) {
	t.Parallel()

	// Create DZD offset
	dzdKeypair := solana.NewWallet().PrivateKey
	dzdSigner := NewOffsetSigner(dzdKeypair)

	dzdOffset := &LocationOffset{
		MeasurementSlot: 100,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   800000,
		RttNs:           800000,
		NumReferences:   0,
		References:      nil,
	}

	err := dzdSigner.SignOffset(dzdOffset)
	require.NoError(t, err)

	// Tamper with the DZD offset after signing
	TamperOffset(dzdOffset)

	// Create Probe offset with tampered reference
	probeKeypair := solana.NewWallet().PrivateKey
	probeSigner := NewOffsetSigner(probeKeypair)

	probeOffset := &LocationOffset{
		MeasurementSlot: 101,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   12500000,
		RttNs:           13300000,
		NumReferences:   1,
		References:      []LocationOffset{*dzdOffset},
	}

	err = probeSigner.SignOffset(probeOffset)
	require.NoError(t, err)

	// Chain verification should fail (reference signature is invalid)
	err = VerifyOffsetChain(probeOffset)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to verify reference 0")
}

func TestVerifyOffsetChain_MultiLevel(t *testing.T) {
	t.Parallel()

	// Create a 3-level chain: DZD -> Probe1 -> Probe2
	dzdKeypair := solana.NewWallet().PrivateKey
	dzdSigner := NewOffsetSigner(dzdKeypair)

	dzdOffset := &LocationOffset{
		MeasurementSlot: 100,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   500000,
		RttNs:           500000,
		NumReferences:   0,
		References:      nil,
	}

	err := dzdSigner.SignOffset(dzdOffset)
	require.NoError(t, err)

	probe1Keypair := solana.NewWallet().PrivateKey
	probe1Signer := NewOffsetSigner(probe1Keypair)

	probe1Offset := &LocationOffset{
		MeasurementSlot: 101,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   1000000,
		RttNs:           1500000,
		NumReferences:   1,
		References:      []LocationOffset{*dzdOffset},
	}

	err = probe1Signer.SignOffset(probe1Offset)
	require.NoError(t, err)

	probe2Keypair := solana.NewWallet().PrivateKey
	probe2Signer := NewOffsetSigner(probe2Keypair)

	probe2Offset := &LocationOffset{
		MeasurementSlot: 102,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   2000000,
		RttNs:           3500000,
		NumReferences:   1,
		References:      []LocationOffset{*probe1Offset},
	}

	err = probe2Signer.SignOffset(probe2Offset)
	require.NoError(t, err)

	// Verify the entire 3-level chain
	err = VerifyOffsetChain(probe2Offset)
	require.NoError(t, err)
}

func TestOffsetSigner_GetPublicKey(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	signer := NewOffsetSigner(keypair)

	pubkey := signer.GetPublicKey()
	require.Equal(t, keypair.PublicKey(), pubkey)
}

func TestOffsetSignaturesEqual(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	signer := NewOffsetSigner(keypair)

	offset1 := &LocationOffset{
		MeasurementSlot: 12345,
		Lat:             1.0,
		Lng:             2.0,
		MeasuredRttNs:   1000,
		RttNs:           1000,
		NumReferences:   0,
		References:      nil,
	}

	offset2 := &LocationOffset{
		MeasurementSlot: 12345,
		Lat:             1.0,
		Lng:             2.0,
		MeasuredRttNs:   1000,
		RttNs:           1000,
		NumReferences:   0,
		References:      nil,
	}

	// Sign both with the same data
	err := signer.SignOffset(offset1)
	require.NoError(t, err)

	err = signer.SignOffset(offset2)
	require.NoError(t, err)

	// Signatures should be equal (same data, same key)
	require.True(t, OffsetSignaturesEqual(offset1, offset2))

	// Modify one offset and re-sign
	offset2.MeasuredRttNs = 2000
	err = signer.SignOffset(offset2)
	require.NoError(t, err)

	// Signatures should now differ
	require.False(t, OffsetSignaturesEqual(offset1, offset2))
}

func TestSignOffset_RoundTrip(t *testing.T) {
	t.Parallel()

	// Create offset, sign, marshal, unmarshal, verify
	keypair := solana.NewWallet().PrivateKey
	signer := NewOffsetSigner(keypair)

	original := &LocationOffset{
		MeasurementSlot: 99999,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   800000,
		RttNs:           800000,
		NumReferences:   0,
		References:      nil,
	}

	// Sign
	err := signer.SignOffset(original)
	require.NoError(t, err)

	// Marshal
	data, err := original.Marshal()
	require.NoError(t, err)

	// Unmarshal
	decoded := &LocationOffset{}
	err = decoded.Unmarshal(data)
	require.NoError(t, err)

	// Verify signature on decoded offset
	err = VerifyOffset(decoded)
	require.NoError(t, err)

	// Ensure signatures match
	require.True(t, OffsetSignaturesEqual(original, decoded))
}

func TestVerifyOffset_EmptySignature(t *testing.T) {
	t.Parallel()

	offset := &LocationOffset{
		Signature:       [64]byte{}, // Empty signature
		Pubkey:          [32]byte{1, 2, 3},
		MeasurementSlot: 1,
		Lat:             1.0,
		Lng:             2.0,
		MeasuredRttNs:   1000,
		RttNs:           1000,
		NumReferences:   0,
		References:      nil,
	}

	// Verification should fail
	err := VerifyOffset(offset)
	require.Error(t, err)
}
