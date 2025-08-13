package funder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestFunder_LoadRecipientsFromJSONFile(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "recipients.json")
		file, err := os.Create(path)
		require.NoError(t, err)
		defer file.Close()

		recipient1 := solana.NewWallet().PublicKey()
		recipient2 := solana.NewWallet().PublicKey()

		err = json.NewEncoder(file).Encode(map[string]solana.PublicKey{
			"recipient1": recipient1,
			"recipient2": recipient2,
		})
		require.NoError(t, err)

		recipients, err := LoadRecipientsFromJSONFile(path)
		require.NoError(t, err)
		require.Equal(t, 2, len(recipients))

		require.Equal(t, recipient1, recipients["recipient1"])
		require.Equal(t, recipient2, recipients["recipient2"])
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "recipients.json")
		file, err := os.Create(path)
		require.NoError(t, err)
		defer file.Close()

		err = json.NewEncoder(file).Encode(map[string]string{
			"recipient1": "invalid",
		})
		require.NoError(t, err)

		_, err = LoadRecipientsFromJSONFile(path)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid base58")
	})

	t.Run("invalid pubkey", func(t *testing.T) {
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "recipients.json")
		file, err := os.Create(path)
		require.NoError(t, err)
		defer file.Close()

		err = json.NewEncoder(file).Encode(map[string]string{
			"recipient1": "invalid1",
		})
		require.NoError(t, err)

		_, err = LoadRecipientsFromJSONFile(path)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid public key")
	})
}
