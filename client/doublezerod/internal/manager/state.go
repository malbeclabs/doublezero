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
}

// LoadOrMigrateState loads the reconciler enabled state from the state file.
// If the state file doesn't exist, it checks for the old doublezerod.json file
// (migration from pre-reconciler daemon) and enables the reconciler only if
// the file contains actual provision entries (active tunnels).
// If neither file exists, it's a fresh install and defaults to disabled.
func LoadOrMigrateState(stateDir string) (bool, error) {
	statePath := filepath.Join(stateDir, stateFileName)

	data, err := os.ReadFile(statePath)
	if err == nil {
		var state State
		if err := json.Unmarshal(data, &state); err != nil {
			return false, fmt.Errorf("error parsing state file: %w", err)
		}
		return state.ReconcilerEnabled, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("error reading state file: %w", err)
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
			return false, fmt.Errorf("error removing old state file: %w", err)
		}
		if err := WriteState(stateDir, enabled); err != nil {
			return false, err
		}
		return enabled, nil
	}

	// Fresh install
	if err := WriteState(stateDir, false); err != nil {
		return false, err
	}
	return false, nil
}

// WriteState writes the reconciler enabled state to the state file atomically.
// It writes to a temporary file and renames it to prevent corruption on crash.
func WriteState(stateDir string, enabled bool) error {
	statePath := filepath.Join(stateDir, stateFileName)
	state := State{ReconcilerEnabled: enabled}
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
