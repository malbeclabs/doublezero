package tailer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// helper to append a string to a file.
func appendStr(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestPollMissingFile covers the pre-existence case: Poll returns nil
// without error when the file does not exist yet.
func TestPollMissingFile(t *testing.T) {
	dir := t.TempDir()
	tl := New(filepath.Join(dir, "absent.log"))
	defer tl.Close()
	lines, err := tl.Poll()
	if err != nil {
		t.Fatalf("Poll missing: %v", err)
	}
	if lines != nil {
		t.Fatalf("Poll missing returned %v, want nil", lines)
	}
}

// TestPollAppend covers the basic happy path: open, return complete lines,
// then resume from the offset on the next Poll.
func TestPollAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tail.log")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tl := New(path)
	defer tl.Close()
	got, err := tl.Poll()
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Poll = %v, want [a b]", got)
	}
	appendStr(t, path, "c\nd\n")
	got, err = tl.Poll()
	if err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Fatalf("Poll 2 = %v, want [c d]", got)
	}
}

// TestPollPartialLine: data without trailing '\n' is buffered until the
// terminator arrives.
func TestPollPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tail.log")
	appendStr(t, path, "abc")
	tl := New(path)
	defer tl.Close()
	got, err := tl.Poll()
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got != nil {
		t.Fatalf("Poll partial = %v, want nil", got)
	}
	appendStr(t, path, "def\n")
	got, err = tl.Poll()
	if err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if len(got) != 1 || got[0] != "abcdef" {
		t.Fatalf("Poll 2 = %v, want [abcdef]", got)
	}
}

// TestPollTruncate: shrinking the file size between polls forces a reopen
// from offset zero and the partial buffer is dropped.
func TestPollTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tail.log")
	if err := os.WriteFile(path, []byte("longer-old\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tl := New(path)
	defer tl.Close()
	if _, err := tl.Poll(); err != nil {
		t.Fatalf("Poll 1: %v", err)
	}
	// Write a strictly shorter body so size < offset triggers reopen.
	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	got, err := tl.Poll()
	if err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if len(got) != 1 || got[0] != "new" {
		t.Fatalf("Poll 2 = %v, want [new]", got)
	}
}

// TestPollRotation: renaming the file and creating a new one with the
// same path is detected via inode change and the new file is read from
// the start.
func TestPollRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tail.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tl := New(path)
	defer tl.Close()
	if _, err := tl.Poll(); err != nil {
		t.Fatalf("Poll 1: %v", err)
	}
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := os.WriteFile(path, []byte("fresh\n"), 0o644); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	got, err := tl.Poll()
	if err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("Poll 2 = %v, want [fresh]", got)
	}
}

// TestPollOversizeLine: an unterminated line larger than maxPartialBytes
// is dropped and ErrOversizeLine is returned, but any complete lines that
// preceded it are still returned to the caller.
func TestPollOversizeLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tail.log")
	// One complete line followed by a single oversize fragment (>1 MiB,
	// no newline).
	huge := make([]byte, maxPartialBytes+1024)
	for i := range huge {
		huge[i] = 'x'
	}
	if err := os.WriteFile(path, append([]byte("good\n"), huge...), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tl := New(path)
	defer tl.Close()
	got, err := tl.Poll()
	if !errors.Is(err, ErrOversizeLine) {
		t.Fatalf("Poll err = %v, want ErrOversizeLine", err)
	}
	if len(got) != 1 || got[0] != "good" {
		t.Fatalf("Poll = %v, want [good]", got)
	}
}

// TestPollEOFMidLine: partial data plus a complete line in the same
// poll must surface only the complete one.
func TestPollEOFMidLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tail.log")
	if err := os.WriteFile(path, []byte("done\npart"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tl := New(path)
	defer tl.Close()
	got, err := tl.Poll()
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(got) != 1 || got[0] != "done" {
		t.Fatalf("Poll = %v, want [done]", got)
	}
}
