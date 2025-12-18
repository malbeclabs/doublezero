package state

import (
	"context"

	"github.com/gagliardetto/solana-go"
)

func NewKeypairSigner(keypair solana.PrivateKey) *KeypairSigner {
	return &KeypairSigner{keypair: keypair}
}

type KeypairSigner struct {
	keypair solana.PrivateKey
}

func (s *KeypairSigner) Sign(ctx context.Context, data []byte) ([]byte, error) {
	signature, err := s.keypair.Sign(data)
	if err != nil {
		return nil, err
	}
	return signature[:], nil
}
