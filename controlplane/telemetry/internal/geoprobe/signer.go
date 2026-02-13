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
