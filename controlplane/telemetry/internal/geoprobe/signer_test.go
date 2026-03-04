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
	senderPubkey := solana.NewWallet().PublicKey()
	signer, err := NewOffsetSigner(keypair, senderPubkey)
	require.NoError(t, err)

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
	err = signer.SignOffset(offset)
	require.NoError(t, err)

	// Verify signature is not empty
	require.NotEqual(t, [64]byte{}, offset.Signature)

	// Verify authority pubkey was set correctly
	expectedPubkey := keypair.PublicKey()
	require.Equal(t, expectedPubkey[:], offset.AuthorityPubkey[:])

	// Verify sender pubkey was set correctly
	require.Equal(t, senderPubkey[:], offset.SenderPubkey[:])

	// Verify the signature is valid
	err = VerifyOffset(offset)
	require.NoError(t, err)
}

func TestVerifyOffset_InvalidSignature(t *testing.T) {
	t.Parallel()

	// Generate a keypair and sign an offset
	keypair := solana.NewWallet().PrivateKey
	senderPubkey := solana.NewWallet().PublicKey()
	signer, err := NewOffsetSigner(keypair, senderPubkey)
	require.NoError(t, err)

	offset := &LocationOffset{
		MeasurementSlot: 99999,
		Lat:             50.1109,
		Lng:             8.6821,
		MeasuredRttNs:   1000000,
		RttNs:           1000000,
		NumReferences:   0,
		References:      nil,
	}

	err = signer.SignOffset(offset)
	require.NoError(t, err)

	// Tamper with the data (invalidates signature)
	tamperOffset(offset)

	// Verification should fail
	err = VerifyOffset(offset)
	require.Error(t, err)
	require.Contains(t, err.Error(), "signature verification failed")
}

func TestVerifyOffset_WrongPublicKey(t *testing.T) {
	t.Parallel()

	// Sign with one keypair
	keypair1 := solana.NewWallet().PrivateKey
	senderPubkey1 := solana.NewWallet().PublicKey()
	signer1, err := NewOffsetSigner(keypair1, senderPubkey1)
	require.NoError(t, err)

	offset := &LocationOffset{
		MeasurementSlot: 55555,
		Lat:             1.0,
		Lng:             2.0,
		MeasuredRttNs:   500000,
		RttNs:           500000,
		NumReferences:   0,
		References:      nil,
	}

	err = signer1.SignOffset(offset)
	require.NoError(t, err)

	// Replace the authority public key with a different one
	keypair2 := solana.NewWallet().PrivateKey
	pubkey2 := keypair2.PublicKey()
	copy(offset.AuthorityPubkey[:], pubkey2[:])

	// Verification should fail (signature doesn't match new pubkey)
	err = VerifyOffset(offset)
	require.Error(t, err)
}

func TestSignOffset_WithReferences(t *testing.T) {
	t.Parallel()

	// Create DZD offset
	dzdKeypair := solana.NewWallet().PrivateKey
	dzdSenderPubkey := solana.NewWallet().PublicKey()
	dzdSigner, err := NewOffsetSigner(dzdKeypair, dzdSenderPubkey)
	require.NoError(t, err)

	dzdOffset := &LocationOffset{
		MeasurementSlot: 100,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   800000,
		RttNs:           800000,
		NumReferences:   0,
		References:      nil,
	}

	err = dzdSigner.SignOffset(dzdOffset)
	require.NoError(t, err)

	// Create Probe offset that references DZD
	probeKeypair := solana.NewWallet().PrivateKey
	probeSenderPubkey := solana.NewWallet().PublicKey()
	probeSigner, err := NewOffsetSigner(probeKeypair, probeSenderPubkey)
	require.NoError(t, err)

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
	dzdSenderPubkey := solana.NewWallet().PublicKey()
	dzdSigner, err := NewOffsetSigner(dzdKeypair, dzdSenderPubkey)
	require.NoError(t, err)

	dzdOffset := &LocationOffset{
		MeasurementSlot: 100,
		Lat:             50.1109,
		Lng:             8.6821,
		MeasuredRttNs:   800000,
		RttNs:           800000,
		NumReferences:   0,
		References:      nil,
	}

	err = dzdSigner.SignOffset(dzdOffset)
	require.NoError(t, err)

	probeKeypair := solana.NewWallet().PrivateKey
	probeSenderPubkey := solana.NewWallet().PublicKey()
	probeSigner, err := NewOffsetSigner(probeKeypair, probeSenderPubkey)
	require.NoError(t, err)

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
	dzdSenderPubkey := solana.NewWallet().PublicKey()
	dzdSigner, err := NewOffsetSigner(dzdKeypair, dzdSenderPubkey)
	require.NoError(t, err)

	dzdOffset := &LocationOffset{
		MeasurementSlot: 100,
		Lat:             52.3676,
		Lng:             4.9041,
		MeasuredRttNs:   800000,
		RttNs:           800000,
		NumReferences:   0,
		References:      nil,
	}

	err = dzdSigner.SignOffset(dzdOffset)
	require.NoError(t, err)

	// Tamper with the DZD offset after signing
	tamperOffset(dzdOffset)

	// Create Probe offset with tampered reference
	probeKeypair := solana.NewWallet().PrivateKey
	probeSenderPubkey := solana.NewWallet().PublicKey()
	probeSigner, err := NewOffsetSigner(probeKeypair, probeSenderPubkey)
	require.NoError(t, err)

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

func TestOffsetSigner_GetPublicKey(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	senderPubkey := solana.NewWallet().PublicKey()
	signer, err := NewOffsetSigner(keypair, senderPubkey)
	require.NoError(t, err)

	pubkey := signer.GetPublicKey()
	require.Equal(t, keypair.PublicKey(), pubkey)
}

func TestOffsetSignaturesEqual(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	senderPubkey := solana.NewWallet().PublicKey()
	signer, err := NewOffsetSigner(keypair, senderPubkey)
	require.NoError(t, err)

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
	err = signer.SignOffset(offset1)
	require.NoError(t, err)

	err = signer.SignOffset(offset2)
	require.NoError(t, err)

	// Signatures should be equal (same data, same key)
	require.True(t, offsetSignaturesEqual(offset1, offset2))

	// Modify one offset and re-sign
	offset2.MeasuredRttNs = 2000
	err = signer.SignOffset(offset2)
	require.NoError(t, err)

	// Signatures should now differ
	require.False(t, offsetSignaturesEqual(offset1, offset2))
}

func TestSignOffset_RoundTrip(t *testing.T) {
	t.Parallel()

	// Create offset, sign, marshal, unmarshal, verify
	keypair := solana.NewWallet().PrivateKey
	senderPubkey := solana.NewWallet().PublicKey()
	signer, err := NewOffsetSigner(keypair, senderPubkey)
	require.NoError(t, err)

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
	err = signer.SignOffset(original)
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
	require.True(t, offsetSignaturesEqual(original, decoded))
}

func TestVerifyOffset_EmptySignature(t *testing.T) {
	t.Parallel()

	offset := &LocationOffset{
		Signature:       [64]byte{}, // Empty signature
		AuthorityPubkey: [32]byte{1, 2, 3},
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

func TestVerifyOffset_TamperedSenderPubkey(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	senderPubkey := solana.NewWallet().PublicKey()
	signer, err := NewOffsetSigner(keypair, senderPubkey)
	require.NoError(t, err)

	offset := &LocationOffset{
		MeasurementSlot: 12345,
		Lat:             1.0,
		Lng:             2.0,
		MeasuredRttNs:   1000,
		RttNs:           1000,
		NumReferences:   0,
		References:      nil,
	}

	err = signer.SignOffset(offset)
	require.NoError(t, err)

	// Tamper with SenderPubkey after signing
	differentPubkey := solana.NewWallet().PublicKey()
	copy(offset.SenderPubkey[:], differentPubkey[:])

	// Verification should fail (SenderPubkey is covered by the signature)
	err = VerifyOffset(offset)
	require.Error(t, err)
	require.Contains(t, err.Error(), "signature verification failed")
}

func TestNewOffsetSigner_ZeroSenderPubkey(t *testing.T) {
	t.Parallel()

	keypair := solana.NewWallet().PrivateKey
	zeroPubkey := solana.PublicKey{}

	_, err := NewOffsetSigner(keypair, zeroPubkey)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sender pubkey must not be zero")
}
