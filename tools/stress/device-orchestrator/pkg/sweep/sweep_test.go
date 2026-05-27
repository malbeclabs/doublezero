package sweep_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/agent"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/runlog"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/sweep"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock provides deterministic Now() and a manually-fired After channel so
// the sweep's hold call returns instantly under test.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	holds int
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(time.Microsecond) // advance so successive Now() calls differ
	return f.now
}

func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	f.holds++
	f.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

func (f *fakeClock) HoldCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.holds
}

// fakeExecutor records create/delete calls. ListUsers always returns the
// orchestrator-owned set so PlanFor produces the right delta.
type fakeExecutor struct {
	mu      sync.Mutex
	owner   solana.PublicKey
	created []serviceability.User
	createN atomic.Int32
	deleteN atomic.Int32

	// Optional hook to fail on the Nth create (1-based) — used by the abort test.
	failCreateOnCall int
	failErr          error
}

func newFakeExecutor(owner solana.PublicKey) *fakeExecutor {
	return &fakeExecutor{owner: owner}
}

func (f *fakeExecutor) ListUsers(ctx context.Context) ([]serviceability.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]serviceability.User, len(f.created))
	copy(out, f.created)
	return out, nil
}

func (f *fakeExecutor) CreateUser(ctx context.Context, idx int) (sweep.CreateResult, error) {
	calls := int(f.createN.Add(1))
	if f.failCreateOnCall == calls && f.failErr != nil {
		return sweep.CreateResult{}, f.failErr
	}

	// Deterministic pubkey from idx, IP = 100.0.0.idx+1 so PlanFor sorts cleanly.
	var pk solana.PublicKey
	pk[0] = byte(idx)
	pk[31] = 0xAA

	f.mu.Lock()
	f.created = append(f.created, serviceability.User{
		Owner:    f.owner,
		ClientIp: [4]byte{100, 0, 0, byte(idx + 1)},
		PubKey:   pk,
	})
	f.mu.Unlock()

	now := time.Unix(1_700_000_000, int64(calls)*1_000_000) // micro-spaced timestamps
	return sweep.CreateResult{
		UserPDA:     pk,
		TunnelID:    uint16(500 + idx),
		ConfirmedAt: now,
		ActivatedAt: now.Add(time.Millisecond),
	}, nil
}

func (f *fakeExecutor) DeleteUser(ctx context.Context, userPDA solana.PublicKey) (sweep.DeleteResult, error) {
	calls := int(f.deleteN.Add(1))
	f.mu.Lock()
	// Remove the matching user from the active set.
	for i, u := range f.created {
		if solana.PublicKeyFromBytes(u.PubKey[:]).Equals(userPDA) {
			f.created = append(f.created[:i], f.created[i+1:]...)
			break
		}
	}
	f.mu.Unlock()

	now := time.Unix(1_700_000_000, int64(calls+1000)*1_000_000)
	return sweep.DeleteResult{
		ConfirmedAt: now,
		ActivatedAt: now.Add(time.Millisecond),
	}, nil
}

func readRows(t *testing.T, path string) []runlog.Row {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	var rows []runlog.Row
	s := bufio.NewScanner(f)
	for s.Scan() {
		var r runlog.Row
		require.NoError(t, json.Unmarshal(s.Bytes(), &r))
		rows = append(rows, r)
	}
	require.NoError(t, s.Err())
	return rows
}

func TestRun_ProvisionsThenDeprovisionsInReverseOrder(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	path := filepath.Join(t.TempDir(), "orchestrator-runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	cfg := sweep.Config{
		RunID:         "run-test",
		Target:        4,
		UsersPerBatch: 2,
		Hold:          10 * time.Second,
		OwnerFilter:   owner,
		Executor:      exec,
		Agent:         agent.NewNoop(nil),
		Runlog:        w,
		Clock:         clk,
	}
	require.NoError(t, sweep.Run(context.Background(), cfg))
	require.NoError(t, w.Close())

	rows := readRows(t, path)
	// 4 provisions × 3 events + 4 deprovisions × 3 events = 24 rows
	require.Len(t, rows, 24)

	// Provision phase: ascending user_index, events submit→confirm→activate.
	for i := 0; i < 4; i++ {
		base := i * 3
		assert.Equal(t, i, rows[base].UserIndex, "row %d", base)
		assert.Equal(t, runlog.EventSubmit, rows[base].Event)
		assert.Equal(t, runlog.EventConfirm, rows[base+1].Event)
		assert.Equal(t, runlog.EventActivate, rows[base+2].Event)
		assert.Equal(t, uint16(500+i), rows[base+1].TunnelID, "tunnel_id propagates after confirm")
		assert.Equal(t, i+1, rows[base+2].NAfterEvent, "activate increments active count")
	}

	// Deprovision phase: descending user_index (reverse creation order), events deprovision_submit/confirm/activate.
	for k := 0; k < 4; k++ {
		base := 12 + k*3
		expectedIdx := 3 - k // 3, 2, 1, 0
		assert.Equal(t, expectedIdx, rows[base].UserIndex)
		assert.Equal(t, runlog.EventDeprovisionSubmit, rows[base].Event)
		assert.Equal(t, runlog.EventDeprovisionConfirm, rows[base+1].Event)
		assert.Equal(t, runlog.EventDeprovisionActivate, rows[base+2].Event)
		assert.Equal(t, 3-k, rows[base+2].NAfterEvent, "deprovision_activate decrements active count")
	}

	// Hold called between batches but not after the final provision batch.
	// Target=4, UsersPerBatch=2 → batches at [0..2), [2..4); one hold between them.
	assert.Equal(t, 1, clk.HoldCount(), "Hold should fire once (between batches), not after reaching target")

	// Executor calls match the totals.
	assert.Equal(t, int32(4), exec.createN.Load())
	assert.Equal(t, int32(4), exec.deleteN.Load())
}

func TestRun_HandlesZeroTarget(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	path := filepath.Join(t.TempDir(), "runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	cfg := sweep.Config{
		RunID:         "run-zero",
		Target:        0,
		UsersPerBatch: 2,
		Hold:          time.Second,
		OwnerFilter:   owner,
		Executor:      exec,
		Runlog:        w,
		Clock:         &fakeClock{now: time.Unix(1, 0)},
	}
	require.NoError(t, sweep.Run(context.Background(), cfg))
	require.NoError(t, w.Close())

	rows := readRows(t, path)
	assert.Empty(t, rows)
	assert.Zero(t, exec.createN.Load())
	assert.Zero(t, exec.deleteN.Load())
}

func TestRun_AbortBetweenUsersStillCleansUp(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	exec.failCreateOnCall = 3
	exec.failErr = context.Canceled

	path := filepath.Join(t.TempDir(), "runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	cfg := sweep.Config{
		RunID:         "run-abort",
		Target:        4,
		UsersPerBatch: 2,
		Hold:          time.Second,
		OwnerFilter:   owner,
		Executor:      exec,
		Runlog:        w,
		Clock:         &fakeClock{now: time.Unix(1, 0)},
	}
	err = sweep.Run(context.Background(), cfg)
	require.Error(t, err, "abort during provision should surface error")

	// Even on abort, deprovision should fire for the two users that were created.
	require.NoError(t, w.Close())
	rows := readRows(t, path)

	// 2 provisions × 3 events = 6; plus a submit event for the failed third; plus 2 deprovision sets.
	deprovisionActivates := 0
	for _, r := range rows {
		if r.Event == runlog.EventDeprovisionActivate {
			deprovisionActivates++
		}
	}
	assert.Equal(t, 2, deprovisionActivates, "every created user should be deprovisioned on abort")
}

func TestRun_RejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  sweep.Config
	}{
		{name: "negative target", cfg: sweep.Config{Target: -1, UsersPerBatch: 1, RunID: "r", Executor: &fakeExecutor{}, Runlog: &runlog.Writer{}}},
		{name: "zero batch", cfg: sweep.Config{Target: 1, UsersPerBatch: 0, RunID: "r", Executor: &fakeExecutor{}, Runlog: &runlog.Writer{}}},
		{name: "missing run id", cfg: sweep.Config{Target: 1, UsersPerBatch: 1, Executor: &fakeExecutor{}, Runlog: &runlog.Writer{}}},
		{name: "missing executor", cfg: sweep.Config{Target: 1, UsersPerBatch: 1, RunID: "r", Runlog: &runlog.Writer{}}},
		{name: "missing runlog", cfg: sweep.Config{Target: 1, UsersPerBatch: 1, RunID: "r", Executor: &fakeExecutor{}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := sweep.Run(context.Background(), tc.cfg)
			require.Error(t, err)
		})
	}
}

// Sanity: ctx cancellation between users is observed at the next iteration boundary.
func TestRun_CancellationStopsBetweenUsers(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	path := filepath.Join(t.TempDir(), "runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	cfg := sweep.Config{
		RunID:         "run-cancel",
		Target:        4,
		UsersPerBatch: 2,
		Hold:          time.Second,
		OwnerFilter:   owner,
		Executor:      exec,
		Runlog:        w,
		Clock:         &fakeClock{now: time.Unix(1, 0)},
	}
	err = sweep.Run(ctx, cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Zero(t, exec.createN.Load(), "no users should be created when ctx is pre-cancelled")
}
