package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
)

const (
	stateFileName    = "state.json"
	oldStateFileName = "doublezerod.json"
)

// State represents the persistent reconciler state.
type State struct {
	ReconcilerEnabled bool `json:"reconciler_enabled"`
	// ClientIP is an operator-pinned address, empty when the daemon should use the one it
	// discovers at startup. It is persisted for the same reason ReconcilerEnabled is: the
	// daemon restores itself after a restart without the CLI being run again, so a pin that
	// did not survive would silently revert the host to its discovered address, stop matching
	// the onchain user, and tear the tunnel down on the next reboot.
	//
	// It is host configuration, not session state: only a new pin replaces it, and neither a
	// disable nor an enable without one clears it. Returning a host to discovery means
	// pinning a different address, or clearing this field and restarting the daemon.
	ClientIP string `json:"client_ip,omitempty"`
}

// LoadOrMigrateState loads the reconciler enabled state from the state file.
// If the state file doesn't exist, it checks for the old doublezerod.json file
// (migration from pre-reconciler daemon) and enables the reconciler only if
// the file contains actual provision entries (active tunnels).
// If neither file exists, it's a fresh install and defaults to disabled.
func LoadOrMigrateState(stateDir string) (State, error) {
	statePath := filepath.Join(stateDir, stateFileName)

	data, err := os.ReadFile(statePath)
	if err == nil {
		var state State
		if err := json.Unmarshal(data, &state); err != nil {
			return State{}, fmt.Errorf("error parsing state file: %w", err)
		}
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return State{}, fmt.Errorf("error reading state file: %w", err)
	}

	// Check for old doublezerod.json (migration from pre-reconciler daemon).
	// Only enable the reconciler if the old file contains actual provision entries,
	// meaning the client had active tunnels. The old daemon always created this file
	// on startup, so its mere existence doesn't imply a connected state.
	oldPath := filepath.Join(stateDir, oldStateFileName)
	if oldData, err := os.ReadFile(oldPath); err == nil {
		enabled := false
		var entries []*api.ProvisionRequest
		if json.Unmarshal(oldData, &entries) == nil && len(entries) > 0 {
			enabled = true
		}
		if err := os.Remove(oldPath); err != nil {
			return State{}, fmt.Errorf("error removing old state file: %w", err)
		}
		migrated := State{ReconcilerEnabled: enabled}
		if err := WriteState(stateDir, migrated); err != nil {
			return State{}, err
		}
		return migrated, nil
	}

	// Fresh install
	if err := WriteState(stateDir, State{}); err != nil {
		return State{}, err
	}
	return State{}, nil
}

// WriteState writes the reconciler enabled state to the state file atomically.
// It writes to a temporary file and renames it to prevent corruption on crash.
func WriteState(stateDir string, state State) error {
	statePath := filepath.Join(stateDir, stateFileName)
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("error marshaling state: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("error creating state directory: %w", err)
	}
	tmpPath := statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("error writing temp state file: %w", err)
	}
	if err := os.Rename(tmpPath, statePath); err != nil {
		return fmt.Errorf("error renaming state file: %w", err)
	}
	return nil
}
