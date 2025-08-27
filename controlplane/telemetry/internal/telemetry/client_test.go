package telemetry

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestDeviceTelemetry_Client(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("constructor loads signer and GetSignerPublicKey matches", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path, key := newKeyFile(t, dir)
		programID := solana.NewWallet().PublicKey()

		c, err := NewTelemetryClient(logger, nil, programID, path)
		require.NoError(t, err)

		got, err := c.GetSignerPublicKey()
		require.NoError(t, err)
		require.True(t, got.Equals(key.PublicKey()))
	})

	t.Run("constructor errors when key file missing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "missing.json")
		programID := solana.NewWallet().PublicKey()

		_, err := NewTelemetryClient(logger, nil, programID, path)
		require.Error(t, err)
	})

	t.Run("maybeRefresh no-op when mtime unchanged", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path, key := newKeyFile(t, dir)
		programID := solana.NewWallet().PublicKey()

		c, err := NewTelemetryClient(logger, nil, programID, path)
		require.NoError(t, err)

		// Ensure maybeRefresh will actually stat by making lastStatTime old
		c.mu.Lock()
		c.lastStatTime = time.Time{}
		oldMtime := c.lastKeyMtime
		c.mu.Unlock()

		require.NoError(t, c.maybeRefresh())

		// Signer and cached mtime should be unchanged
		got, err := c.GetSignerPublicKey()
		require.NoError(t, err)
		require.True(t, got.Equals(key.PublicKey()))

		c.mu.RLock()
		require.True(t, c.lastKeyMtime.Equal(oldMtime))
		c.mu.RUnlock()
	})

	t.Run("maybeRefresh updates when mtime advances", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path, _ := newKeyFile(t, dir)
		programID := solana.NewWallet().PublicKey()

		c, err := NewTelemetryClient(logger, nil, programID, path)
		require.NoError(t, err)

		// FS granularity guard (many FS have 1s mtime precision)
		time.Sleep(1100 * time.Millisecond)
		newKey := rotateKeyFile(t, path)

		// Force maybeRefresh to stat
		c.mu.Lock()
		c.lastStatTime = time.Time{}
		c.mu.Unlock()

		require.NoError(t, c.maybeRefresh())

		got, err := c.GetSignerPublicKey()
		require.NoError(t, err)
		require.True(t, got.Equals(newKey.PublicKey()))
	})

	t.Run("throttling skips stat within minStatInterval", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path, key := newKeyFile(t, dir)
		programID := solana.NewWallet().PublicKey()

		c, err := NewTelemetryClient(logger, nil, programID, path, WithMinStatInterval(2*time.Second))
		require.NoError(t, err)

		// Set a long throttle and mark we just stat'ed now
		c.mu.Lock()
		c.lastStatTime = time.Now()
		origMtime := c.lastKeyMtime
		c.mu.Unlock()

		// Rotate key immediately (mtime differs), but throttle should prevent stat
		_ = rotateKeyFile(t, path)

		require.NoError(t, c.maybeRefresh())

		// Should still see old key because stat was throttled
		got, err := c.GetSignerPublicKey()
		require.NoError(t, err)
		require.True(t, got.Equals(key.PublicKey()))

		// Internal mtime should not have been updated
		c.mu.RLock()
		require.True(t, c.lastKeyMtime.Equal(origMtime))
		c.mu.RUnlock()
	})

	t.Run("GetSignerPublicKey returns error when refresh fails", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path, _ := newKeyFile(t, dir)
		programID := solana.NewWallet().PublicKey()

		c, err := NewTelemetryClient(logger, nil, programID, path)
		require.NoError(t, err)

		// Remove key file and clear client so maybeRefresh must touch disk
		require.NoError(t, os.Remove(path))
		c.mu.Lock()
		c.client = nil
		c.lastStatTime = time.Time{}
		c.mu.Unlock()

		_, err = c.GetSignerPublicKey()
		require.Error(t, err)
	})
}

func writeKeyFile(t *testing.T, path string, key solana.PrivateKey) {
	t.Helper()
	var ints []int
	for _, b := range []byte(key) {
		ints = append(ints, int(b))
	}
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, json.NewEncoder(f).Encode(ints))
	require.NoError(t, f.Sync())
}

func newKeyFile(t *testing.T, dir string) (string, solana.PrivateKey) {
	t.Helper()
	k := solana.NewWallet().PrivateKey
	require.NotZero(t, len(k))
	p := filepath.Join(dir, "id.json")
	writeKeyFile(t, p, k)
	return p, k
}

func rotateKeyFile(t *testing.T, path string) solana.PrivateKey {
	t.Helper()
	k := solana.NewWallet().PrivateKey
	require.NotZero(t, len(k))
	tmp := path + ".tmp"
	writeKeyFile(t, tmp, k)
	require.NoError(t, os.Rename(tmp, path))
	return k
}
