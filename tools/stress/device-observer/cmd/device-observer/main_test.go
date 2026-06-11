package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaleAbortRefuses(t *testing.T) {
	dir := t.TempDir()
	abortFile := filepath.Join(dir, "abort")
	if err := os.WriteFile(abortFile, []byte(`{"trigger":"x"}`), 0o640); err != nil {
		t.Fatalf("write fixture sentinel: %v", err)
	}
	err := checkStaleAbort(abortFile, false)
	if err == nil {
		t.Fatal("expected error when stale sentinel exists and --force is not set")
	}
	if !strings.Contains(err.Error(), "stale abort sentinel") {
		t.Fatalf("error %q should mention stale sentinel", err)
	}
	if _, statErr := os.Stat(abortFile); statErr != nil {
		t.Fatalf("sentinel must not be removed without --force: %v", statErr)
	}
}

func TestStaleAbortForceRemoves(t *testing.T) {
	dir := t.TempDir()
	abortFile := filepath.Join(dir, "abort")
	if err := os.WriteFile(abortFile, []byte(`{"trigger":"x"}`), 0o640); err != nil {
		t.Fatalf("write fixture sentinel: %v", err)
	}
	if err := checkStaleAbort(abortFile, true); err != nil {
		t.Fatalf("checkStaleAbort with --force: %v", err)
	}
	if _, statErr := os.Stat(abortFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected sentinel removed with --force, stat: %v", statErr)
	}
}

func TestStaleAbortMissingNoOp(t *testing.T) {
	dir := t.TempDir()
	abortFile := filepath.Join(dir, "abort")
	if err := checkStaleAbort(abortFile, false); err != nil {
		t.Fatalf("missing sentinel must not return an error: %v", err)
	}
	if err := checkStaleAbort(abortFile, true); err != nil {
		t.Fatalf("missing sentinel with --force must not return an error: %v", err)
	}
}
