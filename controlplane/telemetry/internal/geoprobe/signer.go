package geoprobe

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
)

// OffsetSigner signs LocationOffset messages using Ed25519 with Solana keypairs.
type OffsetSigner struct {
	keypair      solana.PrivateKey
	senderPubkey [32]byte
}

func NewOffsetSigner(keypair solana.PrivateKey, senderPubkey solana.PublicKey) (*OffsetSigner, error) {
	if senderPubkey.IsZero() {
		return nil, fmt.Errorf("sender pubkey must not be zero")
	}
	var spk [32]byte
	copy(spk[:], senderPubkey[:])
	return &OffsetSigner{keypair: keypair, senderPubkey: spk}, nil
}

// SignOffset signs the given offset by populating its Signature, AuthorityPubkey,
// and SenderPubkey fields. The signature is computed over all fields except the
// Signature field itself. This modifies the offset in-place.
func (s *OffsetSigner) SignOffset(offset *LocationOffset) error {
	pubkey := s.keypair.PublicKey()
	copy(offset.AuthorityPubkey[:], pubkey[:])
	offset.SenderPubkey = s.senderPubkey

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
	pubkey := solana.PublicKeyFromBytes(offset.AuthorityPubkey[:])

	// No real signature has a zero S half (S = r + k*sec mod L), and every
	// forgery against a small-order authority pubkey does: ed25519.Verify does
	// not screen small-order keys, so with S and R zero the equation reduces to
	// identity = R + [k]A, which holds for the all-zero key on ~24% of messages
	// and for the identity encoding (0x01||00*31) on every message — an unsigned
	// offset would verify. Rejecting a zero S covers the whole class, because
	// [S]B lies in the prime-order subgroup while a small-order R + [k]A lies in
	// the torsion subgroup, and they meet only at the identity.
	if [32]byte(offset.Signature[32:64]) == [32]byte{} {
		return fmt.Errorf("signature scalar is zero")
	}

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
