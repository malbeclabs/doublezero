// Package tailer is a poll-based file tailer for the device-observer log
// consumers. It tracks open file + offset + inode, detects rotation
// (inode change) and truncation (size shrink), and buffers partial
// trailing data (no newline) until a subsequent Poll completes the line.
//
// Linux-only: rotation detection uses syscall.Stat_t.Ino, which matches the
// rest of the device-observer's Linux-only assumptions.
package tailer

import (
	"bytes"
	"errors"
	"io"
	"os"
	"syscall"
)

// readChunkSize is the size of the per-Poll read buffer. The agent log and
// runlog are short-line files; 32 KiB amortizes syscalls without holding
// excess memory.
const readChunkSize = 32 * 1024

// Tailer reads newly-appended complete lines from a file across multiple
// Poll calls. It is not safe for concurrent use; callers own the polling
// goroutine.
type Tailer struct {
	path    string
	f       *os.File
	inode   uint64
	offset  int64
	partial []byte
}

// New returns a Tailer for path. The file is not opened until the first
// Poll; a missing file is not an error.
func New(path string) *Tailer { return &Tailer{path: path} }

// Poll returns newly completed lines (without trailing '\n') since the
// previous call. Returns nil, nil before the file exists. On rotation
// (inode change) or truncation (size shrink) the tailer reopens from
// offset zero and the held partial fragment is discarded.
func (t *Tailer) Poll() ([]string, error) {
	if t.f == nil {
		if err := t.open(); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
	}
	info, err := os.Stat(t.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errors.New("tailer: cannot extract inode")
	}
	if sys.Ino != t.inode || info.Size() < t.offset {
		_ = t.f.Close()
		t.f = nil
		t.partial = nil
		if err := t.open(); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
	}
	buf := make([]byte, readChunkSize)
	var lines []string
	for {
		n, err := t.f.Read(buf)
		if n > 0 {
			t.offset += int64(n)
			t.partial = append(t.partial, buf[:n]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return lines, err
		}
	}
	for {
		idx := bytes.IndexByte(t.partial, '\n')
		if idx < 0 {
			break
		}
		lines = append(lines, string(t.partial[:idx]))
		t.partial = t.partial[idx+1:]
	}
	// Re-compact partial so the underlying capacity does not grow without
	// bound across many polls on a steadily-appended file.
	if cap(t.partial) > 4*readChunkSize {
		t.partial = append([]byte(nil), t.partial...)
	}
	return lines, nil
}

func (t *Tailer) open() error {
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		_ = f.Close()
		return errors.New("tailer: cannot extract inode")
	}
	t.f = f
	t.inode = sys.Ino
	t.offset = 0
	return nil
}

// Close releases the underlying file handle. Safe to call multiple times.
func (t *Tailer) Close() error {
	if t.f == nil {
		return nil
	}
	err := t.f.Close()
	t.f = nil
	return err
}
