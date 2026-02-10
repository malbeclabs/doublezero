package geoprobe

import (
	"bytes"
	"fmt"

	"github.com/gagliardetto/solana-go"
)

// OffsetSigner signs LocationOffset messages using Ed25519 with Solana keypairs.
type OffsetSigner struct {
	keypair solana.PrivateKey
}

func NewOffsetSigner(keypair solana.PrivateKey) *OffsetSigner {
	return &OffsetSigner{keypair: keypair}
}

// SignOffset signs the given offset by populating its Signature and Pubkey fields.
// The signature is computed over all fields except the Signature field itself.
// This modifies the offset in-place.
func (s *OffsetSigner) SignOffset(offset *LocationOffset) error {
	pubkey := s.keypair.PublicKey()
	copy(offset.Pubkey[:], pubkey[:])

	signingBytes, err := offset.GetSigningBytes()
	if err != nil {
		return fmt.Errorf("failed to get signing bytes: %w", err)
	}

	signature, err := s.keypair.Sign(signingBytes)
	if err != nil {
		return fmt.Errorf("failed to sign offset: %w", err)
	}

	copy(offset.Signature[:], signature[:])

	return nil
}

// VerifyOffset verifies the signature on the given offset matches the data.
// For offsets with references, this only verifies the top-level signature.
// Use VerifyOffsetChain to verify the entire reference chain.
func VerifyOffset(offset *LocationOffset) error {
	pubkey := solana.PublicKeyFromBytes(offset.Pubkey[:])

	signingBytes, err := offset.GetSigningBytes()
	if err != nil {
		return fmt.Errorf("failed to get signing bytes: %w", err)
	}

	var sig solana.Signature
	copy(sig[:], offset.Signature[:])

	if !sig.Verify(pubkey, signingBytes) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

// VerifyOffsetChain verifies the signature on the given offset and recursively
// verifies all reference signatures in the chain.
func VerifyOffsetChain(offset *LocationOffset) error {
	if err := VerifyOffset(offset); err != nil {
		return fmt.Errorf("failed to verify offset signature: %w", err)
	}

	for i, ref := range offset.References {
		if err := VerifyOffsetChain(&ref); err != nil {
			return fmt.Errorf("failed to verify reference %d: %w", i, err)
		}
	}

	return nil
}

func (s *OffsetSigner) GetPublicKey() solana.PublicKey {
	return s.keypair.PublicKey()
}

// TamperOffset modifies an offset's data without updating the signature.
// This is used for testing signature verification failure paths.
// DO NOT USE IN PRODUCTION CODE.
func TamperOffset(offset *LocationOffset) {
	offset.MeasuredRttNs = offset.MeasuredRttNs + 1
}

func OffsetSignaturesEqual(a, b *LocationOffset) bool {
	return bytes.Equal(a.Signature[:], b.Signature[:])
}
