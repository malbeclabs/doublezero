package abort_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/abort"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatch_CancelsWhenAbortFileAppears(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "abort")
	ctx, cancel := abort.Watch(context.Background(), path, 25*time.Millisecond, nil)
	t.Cleanup(cancel)

	// File doesn't exist yet — ctx is alive.
	select {
	case <-ctx.Done():
		t.Fatal("ctx cancelled before abort file existed")
	case <-time.After(50 * time.Millisecond):
	}

	// Touch the abort file.
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	select {
	case <-ctx.Done():
		assert.True(t, errors.Is(ctx.Err(), context.Canceled))
	case <-time.After(time.Second):
		t.Fatal("ctx did not cancel within 1s after abort file touched")
	}
}

func TestWatch_EmptyPathIsNoOp(t *testing.T) {
	t.Parallel()

	parent, parentCancel := context.WithCancel(context.Background())
	t.Cleanup(parentCancel)

	ctx, cancel := abort.Watch(parent, "", 0, nil)
	t.Cleanup(cancel)

	select {
	case <-ctx.Done():
		t.Fatal("empty-path watch should not cancel on its own")
	case <-time.After(50 * time.Millisecond):
	}

	// Parent cancellation still propagates through (we return parent verbatim).
	parentCancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("derived ctx did not pick up parent cancellation")
	}
}

func TestWatch_StopsWhenParentCancelled(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "abort")
	parent, parentCancel := context.WithCancel(context.Background())

	ctx, cancel := abort.Watch(parent, path, 25*time.Millisecond, nil)
	t.Cleanup(cancel)

	parentCancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancel did not propagate")
	}
}
