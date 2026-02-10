package geoprobe

import (
	"bytes"
	"fmt"

	"github.com/gagliardetto/solana-go"
)

// OffsetSigner signs LocationOffset messages using Ed25519 with Solana keypairs.
// This enables cryptographic verification of the measurement chain.
type OffsetSigner struct {
	keypair solana.PrivateKey
}

// NewOffsetSigner creates a new signer with the given Solana keypair.
func NewOffsetSigner(keypair solana.PrivateKey) *OffsetSigner {
	return &OffsetSigner{keypair: keypair}
}

// SignOffset signs the given offset by populating its Signature and Pubkey fields.
// The signature is computed over all fields except the Signature field itself.
//
// This modifies the offset in-place.
func (s *OffsetSigner) SignOffset(offset *LocationOffset) error {
	// Set the pubkey field
	pubkey := s.keypair.PublicKey()
	copy(offset.Pubkey[:], pubkey[:])

	// Get the bytes to sign (everything except signature)
	signingBytes, err := offset.GetSigningBytes()
	if err != nil {
		return fmt.Errorf("failed to get signing bytes: %w", err)
	}

	// Sign the bytes
	signature, err := s.keypair.Sign(signingBytes)
	if err != nil {
		return fmt.Errorf("failed to sign offset: %w", err)
	}

	// Copy the signature into the offset
	copy(offset.Signature[:], signature[:])

	return nil
}

// VerifyOffset verifies the signature on the given offset matches the data.
// It checks that the signature was created by the keypair corresponding to
// the public key in the offset.
//
// For offsets with references, this only verifies the top-level signature.
// Use VerifyOffsetChain to verify the entire reference chain.
func VerifyOffset(offset *LocationOffset) error {
	// Extract the public key from the offset
	pubkey := solana.PublicKeyFromBytes(offset.Pubkey[:])

	// Get the bytes that were signed
	signingBytes, err := offset.GetSigningBytes()
	if err != nil {
		return fmt.Errorf("failed to get signing bytes: %w", err)
	}

	// Convert signature bytes to Solana signature type
	var sig solana.Signature
	copy(sig[:], offset.Signature[:])

	// Verify the signature
	if !sig.Verify(pubkey, signingBytes) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

// VerifyOffsetChain verifies the signature on the given offset and recursively
// verifies all reference signatures in the chain.
func VerifyOffsetChain(offset *LocationOffset) error {
	// Verify this offset's signature
	if err := VerifyOffset(offset); err != nil {
		return fmt.Errorf("failed to verify offset signature: %w", err)
	}

	// Recursively verify all references
	for i, ref := range offset.References {
		if err := VerifyOffsetChain(&ref); err != nil {
			return fmt.Errorf("failed to verify reference %d: %w", i, err)
		}
	}

	return nil
}

// GetPublicKey returns the public key associated with this signer.
func (s *OffsetSigner) GetPublicKey() solana.PublicKey {
	return s.keypair.PublicKey()
}

// TamperOffset modifies an offset's data without updating the signature.
// This is used for testing signature verification failure paths.
// DO NOT USE IN PRODUCTION CODE.
func TamperOffset(offset *LocationOffset) {
	// Modify a field to invalidate the signature
	offset.MeasuredRttNs = offset.MeasuredRttNs + 1
}

// OffsetSignaturesEqual returns true if two offsets have identical signatures.
func OffsetSignaturesEqual(a, b *LocationOffset) bool {
	return bytes.Equal(a.Signature[:], b.Signature[:])
}
