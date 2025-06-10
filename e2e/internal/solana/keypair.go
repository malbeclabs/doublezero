package solana

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mr-tron/base58"
)

func GenerateKeypair(keypairPath string) error {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}

	ints := make([]int, len(priv))
	for i, b := range priv {
		ints[i] = int(b)
	}
	data, err := json.Marshal(ints)
	if err != nil {
		return err
	}

	return os.WriteFile(keypairPath, data, 0600)
}

func PublicAddressFromKeypair(keypairPath string) (string, error) {
	data, err := os.ReadFile(keypairPath)
	if err != nil {
		return "", fmt.Errorf("failed to read keypair file: %w", err)
	}

	var keypair []byte
	if err := json.Unmarshal(data, &keypair); err != nil {
		return "", fmt.Errorf("failed to unmarshal keypair JSON: %w", err)
	}
	if len(keypair) != 64 {
		return "", fmt.Errorf("invalid keypair length: expected 64, got %d", len(keypair))
	}

	pubkey := keypair[32:]
	address := base58.Encode(pubkey)
	return address, nil
}
