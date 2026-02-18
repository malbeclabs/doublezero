package reconciler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
// (migration from pre-reconciler daemon) and treats its presence as enabled.
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

	// Check for old doublezerod.json (migration from pre-reconciler daemon)
	oldPath := filepath.Join(stateDir, oldStateFileName)
	if _, err := os.Stat(oldPath); err == nil {
		if err := os.Remove(oldPath); err != nil {
			return false, fmt.Errorf("error removing old state file: %w", err)
		}
		if err := WriteState(stateDir, true); err != nil {
			return false, err
		}
		return true, nil
	}

	// Fresh install
	if err := WriteState(stateDir, false); err != nil {
		return false, err
	}
	return false, nil
}

// WriteState writes the reconciler enabled state to the state file.
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
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		return fmt.Errorf("error writing state file: %w", err)
	}
	return nil
}
