package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestClient_Config(t *testing.T) {
	t.Parallel()

	t.Run("Load_and_accessors", func(t *testing.T) {
		t.Parallel()
		programID := newProgramID()
		path := writeTempConfig(t, "http://a", programID)

		cfg, err := Load(path)
		require.NoError(t, err)
		require.Equal(t, "http://a", cfg.RPCURL())
		require.Equal(t, programID, cfg.ProgramID())

		require.Eventually(t, func() bool {
			select {
			case <-cfg.Changed():
				return true
			default:
				return false
			}
		}, 2*time.Second, 10*time.Millisecond)
	})

	t.Run("Update_writes_to_disk_and_notifies_once", func(t *testing.T) {
		t.Parallel()
		programID := newProgramID()
		path := writeTempConfig(t, "http://a", programID)

		cfg, err := Load(path)
		require.NoError(t, err)

		newRPC := "http://b"
		_, err = cfg.Update(newRPC, programID)
		require.NoError(t, err)

		onDisk := readConfigFile(t, path)
		require.Equal(t, newRPC, onDisk.LedgerRPCURL)
		require.Equal(t, programID, onDisk.ServiceabilityProgramID)

		require.Eventually(t, func() bool {
			select {
			case <-cfg.Changed():
				return true
			default:
				return false
			}
		}, 2*time.Second, 10*time.Millisecond)

		// No-op update should not notify nor rewrite
		_, err = cfg.Update(newRPC, programID)
		require.NoError(t, err)
		select {
		case <-cfg.Changed():
			t.Fatalf("unexpected signal for no-op update")
		default:
		}
		onDisk2 := readConfigFile(t, path)
		require.Equal(t, onDisk, onDisk2)
	})

	t.Run("Coalesced_notifications_buffer_1", func(t *testing.T) {
		t.Parallel()
		programID := newProgramID()
		path := writeTempConfig(t, "http://a", programID)

		cfg, err := Load(path)
		require.NoError(t, err)

		_, err = cfg.Update("http://b", newProgramID())
		require.NoError(t, err)
		_, err = cfg.Update("http://c", newProgramID()) // back-to-back without draining
		require.NoError(t, err)

		// Only one signal should be queued
		require.Eventually(t, func() bool {
			select {
			case <-cfg.Changed():
				return true
			default:
				return false
			}
		}, 2*time.Second, 10*time.Millisecond)
		select {
		case <-cfg.Changed():
			t.Fatalf("expected only one coalesced signal")
		default:
		}

		// After drain, next update signals again
		_, err = cfg.Update("http://d", newProgramID())
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			select {
			case <-cfg.Changed():
				return true
			default:
				return false
			}
		}, 2*time.Second, 10*time.Millisecond)
	})

	t.Run("Concurrent_updates_coalesce_when_not_drained", func(t *testing.T) {
		t.Parallel()
		programID := newProgramID()
		path := writeTempConfig(t, "http://a", programID)

		cfg, err := Load(path)
		require.NoError(t, err)

		done := make(chan struct{})
		go func() {
			for i := 0; i < 50; i++ {
				rpc := fmt.Sprintf("http://host/%d", i)
				pid := newProgramID()
				_, err := cfg.Update(rpc, pid) // ignore errors for the burst
				require.NoError(t, err)
			}
			close(done)
		}()
		<-done

		// We never drained during updates; buffer size is 1 → exactly one signal
		require.Eventually(t, func() bool {
			select {
			case <-cfg.Changed():
				return true
			default:
				return false
			}
		}, 2*time.Second, 10*time.Millisecond)
		select {
		case <-cfg.Changed():
			t.Fatalf("expected only one coalesced signal after burst")
		default:
		}

		// On-disk must remain valid JSON; exact values depend on last write timing
		_ = readConfigFile(t, path)
	})

	t.Run("Load_missing_file_returns_error", func(t *testing.T) {
		t.Parallel()
		_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
		require.Error(t, err)
	})

	t.Run("Load_malformed_json_returns_error", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "bad.json")
		require.NoError(t, os.WriteFile(p, []byte("{not-json"), 0o644))
		_, err := Load(p)
		require.Error(t, err)
	})

	t.Run("Changed_returns_same_channel_instance", func(t *testing.T) {
		t.Parallel()
		programID := newProgramID()
		path := writeTempConfig(t, "http://a", programID)
		cfg, err := Load(path)
		require.NoError(t, err)
		ch1 := cfg.Changed()
		ch2 := cfg.Changed()
		require.Equal(t, ch1, ch2) // channels are comparable
	})

	t.Run("Atomic_write_never_yields_partial_JSON_during_updates", func(t *testing.T) {
		t.Parallel()
		programID := newProgramID()
		path := writeTempConfig(t, "http://a", programID)
		cfg, err := Load(path)
		require.NoError(t, err)

		// writer: hammer updates
		done := make(chan struct{})
		go func() {
			for i := 0; i < 200; i++ {
				_, err := cfg.Update(fmt.Sprintf("http://host/%d", i), newProgramID())
				require.NoError(t, err)
				time.Sleep(1 * time.Millisecond)
			}
			close(done)
		}()

		// reader: repeatedly read+unmarshal from disk; should always succeed
		for i := 0; i < 400; i++ {
			_ = readConfigFile(t, path) // fails test if invalid/partial JSON is observed
			time.Sleep(500 * time.Microsecond)
		}
		<-done
	})

	t.Run("Concurrent_readers_and_writers_accessors_safe", func(t *testing.T) {
		t.Parallel()
		programID := newProgramID()
		path := writeTempConfig(t, "http://a", programID)
		t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })

		cfg, err := Load(path)
		require.NoError(t, err)

		stop := make(chan struct{})

		// readers
		for r := 0; r < 8; r++ {
			go func() {
				for {
					select {
					case <-stop:
						return
					default:
						_ = cfg.RPCURL()
						_ = cfg.ProgramID()
						time.Sleep(100 * time.Microsecond)
					}
				}
			}()
		}

		// writer (report errors through a channel; don't call require from goroutine)
		writerDone := make(chan error, 1)
		go func() {
			for i := range 100 {
				_, err := cfg.Update(fmt.Sprintf("http://u/%d", i), newProgramID())
				if err != nil {
					writerDone <- err
					close(stop)
					return
				}
				time.Sleep(200 * time.Microsecond)
			}
			close(stop)
			writerDone <- nil
		}()

		// we should receive at least one signal eventually
		require.Eventually(t, func() bool {
			select {
			case <-cfg.Changed():
				return true
			default:
				return false
			}
		}, 2*time.Second, 10*time.Millisecond)

		// ensure the writer finished before the test returns (avoids cleanup racing with writes)
		require.NoError(t, <-writerDone)
	})

}

type diskConfig struct {
	LedgerRPCURL            string           `json:"ledger_rpc_url"`
	ServiceabilityProgramID solana.PublicKey `json:"serviceability_program_id"`
}

func writeTempConfig(t *testing.T, ledgerURL string, programID solana.PublicKey) (path string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "config.json")
	b, err := json.Marshal(diskConfig{LedgerRPCURL: ledgerURL, ServiceabilityProgramID: programID})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o644))
	return path
}

func readConfigFile(t *testing.T, path string) diskConfig {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var c diskConfig
	require.NoError(t, json.Unmarshal(b, &c))
	return c
}

func newProgramID() solana.PublicKey {
	return solana.NewWallet().PublicKey()
}
