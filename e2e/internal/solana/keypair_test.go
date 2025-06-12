package solana_test

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/stretchr/testify/require"
)

func TestGenerateKeypair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keypair.json")

	err := solana.GenerateKeypair(path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var keyJSON []any
	require.NoError(t, json.Unmarshal(data, &keyJSON))
	require.Equal(t, ed25519.PrivateKeySize, len(keyJSON), "expected 64-byte array")

	// Convert []any -> []byte, validating that all elements are integers in [0, 255]
	keyBytes := make([]byte, ed25519.PrivateKeySize)
	for i, v := range keyJSON {
		f, ok := v.(float64)
		require.True(t, ok, "element %d is not a number", i)
		require.GreaterOrEqual(t, f, 0.0, "element %d is negative", i)
		require.LessOrEqual(t, f, 255.0, "element %d exceeds 255", i)
		keyBytes[i] = byte(f)
	}

	priv := ed25519.PrivateKey(keyBytes)
	pubFromPriv := priv.Public().(ed25519.PublicKey)
	expectedPub := keyBytes[32:]

	require.True(t, equalBytes(pubFromPriv, expectedPub), "public key mismatch")
}

func TestPublicAddressFromKeypair(t *testing.T) {
	// Example 64-byte Solana keypair (ed25519 private + public)
	// This one corresponds to pubkey: 7T2Wzq8Km74GZ3HYDpyMRH6nRRZ9yRBYwvvBhfbNNrMf
	keypair := []byte{
		29, 171, 53, 34, 67, 211, 110, 65, 102, 84, 130, 137, 38, 38, 28, 93,
		55, 25, 62, 78, 71, 73, 130, 35, 109, 107, 58, 136, 29, 114, 213, 5,
		213, 40, 182, 163, 124, 25, 195, 52, 201, 132, 140, 90, 85, 251, 162, 240,
		117, 90, 156, 181, 193, 61, 146, 90, 60, 126, 57, 132, 52, 239, 78, 154,
	}

	tmpFile, err := os.CreateTemp("", "keypair.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	data, err := json.Marshal(keypair)
	if err != nil {
		t.Fatalf("Failed to marshal keypair: %v", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	addr, err := solana.PublicAddressFromKeypair(tmpFile.Name())
	if err != nil {
		t.Fatalf("Function returned error: %v", err)
	}

	expected := "FM5r7bfrBWXVFKuSTvPGsLKFuEXsqsu2Uum1BseXNhAh"
	if addr != expected {
		t.Errorf("Expected address %s, got %s", expected, addr)
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
