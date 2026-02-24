package manager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrMigrateState_ExistingStateFile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, stateFileName)

	// Write enabled state
	if err := os.WriteFile(statePath, []byte(`{"reconciler_enabled":true}`), 0644); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("expected enabled=true")
	}
}

func TestLoadOrMigrateState_ExistingStateFileDisabled(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, stateFileName)

	if err := os.WriteFile(statePath, []byte(`{"reconciler_enabled":false}`), 0644); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected enabled=false")
	}
}

func TestLoadOrMigrateState_MigrationFromOldFileWithEntries(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, oldStateFileName)

	// Old doublezerod.json with actual provision entries (client had active tunnels)
	oldContent := `[{"user_type":"IBRL","tunnel_src":"1.2.3.4","tunnel_dst":"5.6.7.8","tunnel_net":"10.0.0.0/30","doublezero_ip":"10.0.0.1","doublezero_prefixes":["10.0.0.0/24"]}]`
	if err := os.WriteFile(oldPath, []byte(oldContent), 0644); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("expected enabled=true after migration from old file with entries")
	}

	// Old file should be removed
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("expected old file to be removed")
	}

	// New state file should exist with enabled=true
	newEnabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !newEnabled {
		t.Fatal("expected enabled=true from new state file")
	}
}

func TestLoadOrMigrateState_MigrationFromOldFileEmpty(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, oldStateFileName)

	// Old doublezerod.json with empty array (no active tunnels)
	if err := os.WriteFile(oldPath, []byte(`[]`), 0644); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected enabled=false — empty old file means no active tunnels")
	}

	// Old file should still be removed
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("expected old file to be removed")
	}
}

func TestLoadOrMigrateState_MigrationFromOldFileEmptyObject(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, oldStateFileName)

	// Old doublezerod.json with empty object (not a valid array of entries)
	if err := os.WriteFile(oldPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected enabled=false — non-array old file means no active tunnels")
	}

	// Old file should still be removed
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("expected old file to be removed")
	}
}

func TestLoadOrMigrateState_MigrationFromOldFileZeroBytes(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, oldStateFileName)

	// Old doublezerod.json that is completely empty (0 bytes)
	if err := os.WriteFile(oldPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected enabled=false — zero-byte old file means no active tunnels")
	}

	// Old file should still be removed
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("expected old file to be removed")
	}
}

func TestLoadOrMigrateState_FreshInstall(t *testing.T) {
	dir := t.TempDir()

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected enabled=false for fresh install")
	}

	// State file should exist with enabled=false
	statePath := filepath.Join(dir, stateFileName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"reconciler_enabled":false}` {
		t.Fatalf("unexpected state file content: %s", data)
	}
}

func TestWriteState(t *testing.T) {
	dir := t.TempDir()

	if err := WriteState(dir, true); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"reconciler_enabled":true}` {
		t.Fatalf("unexpected state file content: %s", data)
	}

	// Overwrite with false
	if err := WriteState(dir, false); err != nil {
		t.Fatal(err)
	}

	data, err = os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"reconciler_enabled":false}` {
		t.Fatalf("unexpected state file content: %s", data)
	}
}

func TestWriteState_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested", "dir")

	if err := WriteState(nested, true); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(nested, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"reconciler_enabled":true}` {
		t.Fatalf("unexpected state file content: %s", data)
	}
}

func TestLoadOrMigrateState_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, stateFileName)

	if err := os.WriteFile(statePath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrMigrateState(dir)
	if err == nil {
		t.Fatal("expected error for empty state file")
	}
}

func TestLoadOrMigrateState_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, stateFileName)

	if err := os.WriteFile(statePath, []byte(`{not valid json}`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrMigrateState(dir)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestLoadOrMigrateState_MigrationWithNonArrayContent(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, oldStateFileName)

	// Old doublezerod.json with non-array content (doesn't parse as []*ProvisionRequest)
	oldContent := `{"tunnel_src":"1.2.3.4","tunnel_dst":"5.6.7.8","user_type":"ibrl"}`
	if err := os.WriteFile(oldPath, []byte(oldContent), 0644); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected enabled=false — non-array content doesn't indicate active tunnels")
	}

	// Old file removed
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("expected old file to be removed")
	}
}

func TestLoadOrMigrateState_StateFileTakesPrecedenceOverOldFile(t *testing.T) {
	dir := t.TempDir()

	// Both files exist — state.json should win
	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte(`{"reconciler_enabled":false}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, oldStateFileName), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	enabled, err := LoadOrMigrateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected enabled=false — state.json should take precedence over old file")
	}

	// Old file should still exist (not cleaned up when state.json is present)
	if _, err := os.Stat(filepath.Join(dir, oldStateFileName)); os.IsNotExist(err) {
		t.Fatal("old file should still exist when state.json is present")
	}
}
