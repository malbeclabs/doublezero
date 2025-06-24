package solana

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/mr-tron/base58"
)

// GenerateKeypair generates a new ed25519 keypair and returns the private key as a byte slice.
func GenerateKeypairJSON() ([]byte, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	ints := make([]int, len(priv))
	for i, b := range priv {
		ints[i] = int(b)
	}

	data, err := json.Marshal(ints)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func PubkeyFromKeypairJSON(keypairJSON []byte) (string, error) {
	var keypair []byte
	if err := json.Unmarshal(keypairJSON, &keypair); err != nil {
		return "", fmt.Errorf("failed to unmarshal keypair JSON: %w", err)
	}

	if len(keypair) != 64 {
		return "", fmt.Errorf("invalid keypair length: expected 64, got %d", len(keypair))
	}

	pubkey := keypair[32:]
	address := base58.Encode(pubkey)
	return address, nil
}
