package sweep_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	// Optional hook invoked after the Nth create completes (1-based), with the
	// created count. The abort test uses it to cancel the sweep context.
	afterCreate func(calls int)

	// Optional gate: when non-nil, DeleteUser blocks on it after incrementing
	// deleteN. Tests use this to interleave work between provision and
	// deprovision (e.g., emitting agent events).
	deleteGate <-chan struct{}
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

	if f.afterCreate != nil {
		f.afterCreate(calls)
	}

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
	if f.deleteGate != nil {
		<-f.deleteGate
	}
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
	path := filepath.Join(t.TempDir(), "orchestrator-runlog.jsonl")
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

	// Provision phase: pipelined per batch of UsersPerBatch=2.
	// Each batch emits all submits first, then confirm/activate pairs in
	// idx order. Two batches × 6 rows = 12 provision rows.
	for k := 0; k < 2; k++ { // batch number
		base := k * 6
		i0 := k * 2 // first user idx in this batch
		// Two submits.
		assert.Equal(t, i0, rows[base].UserIndex, "row %d", base)
		assert.Equal(t, runlog.EventSubmit, rows[base].Event)
		assert.Equal(t, k*2, rows[base].NAfterEvent, "submit reports pre-batch active count")
		assert.Equal(t, i0+1, rows[base+1].UserIndex)
		assert.Equal(t, runlog.EventSubmit, rows[base+1].Event)
		assert.Equal(t, k*2, rows[base+1].NAfterEvent)
		// First user's confirm + activate.
		assert.Equal(t, i0, rows[base+2].UserIndex)
		assert.Equal(t, runlog.EventConfirm, rows[base+2].Event)
		assert.Equal(t, uint16(500+i0), rows[base+2].TunnelID, "tunnel_id propagates after confirm")
		assert.Equal(t, i0, rows[base+3].UserIndex)
		assert.Equal(t, runlog.EventActivate, rows[base+3].Event)
		assert.Equal(t, i0+1, rows[base+3].NAfterEvent, "activate increments active count")
		// Second user's confirm + activate.
		assert.Equal(t, i0+1, rows[base+4].UserIndex)
		assert.Equal(t, runlog.EventConfirm, rows[base+4].Event)
		assert.Equal(t, uint16(500+i0+1), rows[base+4].TunnelID)
		assert.Equal(t, i0+1, rows[base+5].UserIndex)
		assert.Equal(t, runlog.EventActivate, rows[base+5].Event)
		assert.Equal(t, i0+2, rows[base+5].NAfterEvent)
	}

	// Deprovision phase: pipelined per batch of UsersPerBatch=2 in
	// reverse-creation order. Batches are [3,2] then [1,0].
	for k := 0; k < 2; k++ {
		base := 12 + k*6
		i0 := 3 - k*2 // newest in this batch (3 then 1)
		// Two submits.
		assert.Equal(t, i0, rows[base].UserIndex)
		assert.Equal(t, runlog.EventDeprovisionSubmit, rows[base].Event)
		assert.Equal(t, 4-k*2, rows[base].NAfterEvent, "deprovision_submit reports pre-batch active count")
		assert.Equal(t, i0-1, rows[base+1].UserIndex)
		assert.Equal(t, runlog.EventDeprovisionSubmit, rows[base+1].Event)
		// First user's confirm + activate.
		assert.Equal(t, i0, rows[base+2].UserIndex)
		assert.Equal(t, runlog.EventDeprovisionConfirm, rows[base+2].Event)
		assert.Equal(t, i0, rows[base+3].UserIndex)
		assert.Equal(t, runlog.EventDeprovisionActivate, rows[base+3].Event)
		assert.Equal(t, 3-k*2, rows[base+3].NAfterEvent, "deprovision_activate decrements active count")
		// Second user's confirm + activate.
		assert.Equal(t, i0-1, rows[base+4].UserIndex)
		assert.Equal(t, runlog.EventDeprovisionConfirm, rows[base+4].Event)
		assert.Equal(t, i0-1, rows[base+5].UserIndex)
		assert.Equal(t, runlog.EventDeprovisionActivate, rows[base+5].Event)
		assert.Equal(t, 2-k*2, rows[base+5].NAfterEvent)
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
	path := filepath.Join(t.TempDir(), "runlog.jsonl")
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

	path := filepath.Join(t.TempDir(), "runlog.jsonl")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancel the sweep after the 2nd user is created, simulating an abort firing
	// mid-sweep. The next provision iteration observes it at the boundary.
	exec.afterCreate = func(calls int) {
		if calls == 2 {
			cancel()
		}
	}

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
	err = sweep.Run(ctx, cfg)
	require.Error(t, err, "abort during provision should surface error")
	assert.True(t, errors.Is(err, context.Canceled))

	// Even on abort, deprovision must run for the two users that were created —
	// teardown uses an uncancellable context so the cancelled ctx doesn't leak state.
	require.NoError(t, w.Close())
	rows := readRows(t, path)

	assert.Equal(t, int32(2), exec.createN.Load(), "abort stops further creates at the boundary")
	assert.Equal(t, int32(2), exec.deleteN.Load(), "both created users are deprovisioned")
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

	owner := solana.NewWallet().PublicKey()
	tests := []struct {
		name string
		cfg  sweep.Config
	}{
		{name: "negative target", cfg: sweep.Config{Target: -1, UsersPerBatch: 1, RunID: "r", OwnerFilter: owner, Executor: &fakeExecutor{}, Runlog: &runlog.Writer{}}},
		{name: "zero batch", cfg: sweep.Config{Target: 1, UsersPerBatch: 0, RunID: "r", OwnerFilter: owner, Executor: &fakeExecutor{}, Runlog: &runlog.Writer{}}},
		{name: "missing run id", cfg: sweep.Config{Target: 1, UsersPerBatch: 1, OwnerFilter: owner, Executor: &fakeExecutor{}, Runlog: &runlog.Writer{}}},
		{name: "missing owner filter", cfg: sweep.Config{Target: 1, UsersPerBatch: 1, RunID: "r", Executor: &fakeExecutor{}, Runlog: &runlog.Writer{}}},
		{name: "missing executor", cfg: sweep.Config{Target: 1, UsersPerBatch: 1, RunID: "r", OwnerFilter: owner, Runlog: &runlog.Writer{}}},
		{name: "missing runlog", cfg: sweep.Config{Target: 1, UsersPerBatch: 1, RunID: "r", OwnerFilter: owner, Executor: &fakeExecutor{}}},
		{name: "negative quiet window", cfg: sweep.Config{Target: 1, UsersPerBatch: 1, RunID: "r", OwnerFilter: owner, Executor: &fakeExecutor{}, Runlog: &runlog.Writer{}, AgentQuietWindow: -1}},
		{name: "negative quiescence timeout", cfg: sweep.Config{Target: 1, UsersPerBatch: 1, RunID: "r", OwnerFilter: owner, Executor: &fakeExecutor{}, Runlog: &runlog.Writer{}, AgentQuiescenceTimeout: -1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := sweep.Run(context.Background(), tc.cfg)
			require.Error(t, err)
		})
	}
}

// scriptedAgent is an agent.Runner used to drive the sweep's agent-event
// consumer from a test. Events are emitted via Emit() so the test can
// control timing — in production the agent log lags the on-chain CreateUser
// by far longer than registry registration takes, but in tests the executor
// is instantaneous and we need to emit AFTER provision has registered the
// tunnels.
type scriptedAgent struct {
	out      chan agent.Event
	err      error
	closeOut sync.Once
}

func newScriptedAgent() *scriptedAgent {
	return &scriptedAgent{out: make(chan agent.Event, 16)}
}

func (s *scriptedAgent) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.closeOut.Do(func() { close(s.out) })
	}()
	return nil
}

func (s *scriptedAgent) Events() <-chan agent.Event { return s.out }

func (s *scriptedAgent) Err() error { return s.err }

func (s *scriptedAgent) Emit(e agent.Event) { s.out <- e }

// Fail simulates a stream error: it records the terminal error and closes the
// events channel so the consumer observes the failure (as the SSH runner does
// when a read fails). The error is set before the close so the consumer, which
// reads Err() only after the channel drains, sees it.
func (s *scriptedAgent) Fail(err error) {
	s.err = err
	s.closeOut.Do(func() { close(s.out) })
}

func TestRun_ConsumesAgentEventsForRegisteredTunnels(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	// Block deprovision so the test can emit agent events while all created
	// tunnels are registered but before agentCancel() shuts the consumer down.
	gate := make(chan struct{})
	exec.deleteGate = gate

	ag := newScriptedAgent()

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	cfg := sweep.Config{
		RunID:         "run-events",
		Target:        2,
		UsersPerBatch: 2,
		Hold:          0,
		OwnerFilter:   owner,
		Executor:      exec,
		Agent:         ag,
		Runlog:        w,
		Clock:         &fakeClock{now: time.Unix(1_700_000_000, 0)},
	}
	done := make(chan error, 1)
	go func() { done <- sweep.Run(context.Background(), cfg) }()

	// Wait for deprovision to begin (deleteN >= 1) — this means provision is
	// fully complete AND both tunnel registrations are in the registry.
	deadline := time.Now().Add(time.Second)
	for exec.deleteN.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("sweep did not reach deprovision within 1s")
		}
		time.Sleep(time.Millisecond)
	}

	// Emit events for both registered tunnels plus one unregistered one.
	ag.Emit(agent.Event{Kind: agent.EventPreCommitLog, TunnelID: 500, At: time.Unix(1, 100)})
	ag.Emit(agent.Event{Kind: agent.EventApplied, TunnelID: 500, At: time.Unix(1, 200)})
	ag.Emit(agent.Event{Kind: agent.EventPreCommitLog, TunnelID: 999, At: time.Unix(1, 300)}) // unregistered; dropped
	ag.Emit(agent.Event{Kind: agent.EventPreCommitLog, TunnelID: 501, At: time.Unix(1, 400)})
	ag.Emit(agent.Event{Kind: agent.EventApplied, TunnelID: 501, At: time.Unix(1, 500)})

	close(gate) // unblock deprovision

	require.NoError(t, <-done)
	require.NoError(t, w.Close())

	rows := readRows(t, path)

	// Filter for the agent-driven rows so we don't depend on exact interleaving
	// with the submit/confirm/activate stream emitted by provision.
	var preCommit, applied []runlog.Row
	for _, r := range rows {
		switch r.Event {
		case runlog.EventPreCommitLog:
			preCommit = append(preCommit, r)
		case runlog.EventApplied:
			applied = append(applied, r)
		}
	}
	require.Len(t, preCommit, 2, "two registered tunnels → two pre_commit_log rows; the unregistered tunnel 999 is dropped")
	require.Len(t, applied, 2)

	// Tunnel 500 → user_index 0, Tunnel 501 → user_index 1 (fake executor assigns 500+idx).
	for _, r := range preCommit {
		switch r.TunnelID {
		case 500:
			assert.Equal(t, 0, r.UserIndex)
		case 501:
			assert.Equal(t, 1, r.UserIndex)
		default:
			t.Fatalf("unexpected tunnel id %d in pre_commit_log", r.TunnelID)
		}
	}
}

// An agent stream error aborts the run: provision stops, deprovision still
// cleans up what was created, and Run returns the agent error.
func TestRun_AgentStreamErrorFailsRun(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	ag := newScriptedAgent()
	streamErr := errors.New("connection reset")

	// Fail the agent stream after the first create so provision is still in
	// flight when the abort lands.
	exec.afterCreate = func(calls int) {
		if calls == 1 {
			ag.Fail(streamErr)
		}
	}

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	cfg := sweep.Config{
		RunID:         "run-agent-fail",
		Target:        4,
		UsersPerBatch: 1,
		Hold:          0,
		OwnerFilter:   owner,
		Executor:      exec,
		Agent:         ag,
		Runlog:        w,
		Clock:         &fakeClock{now: time.Unix(1_700_000_000, 0)},
	}

	runErr := sweep.Run(context.Background(), cfg)
	require.Error(t, runErr)
	require.ErrorIs(t, runErr, streamErr, "Run should surface the agent stream error")

	// Deprovision still ran: every created user was deleted.
	assert.Equal(t, exec.createN.Load(), exec.deleteN.Load(), "all created users are deprovisioned")
	exec.mu.Lock()
	assert.Empty(t, exec.created, "no users left active after teardown")
	exec.mu.Unlock()
}

// Sanity: ctx cancellation between users is observed at the next iteration boundary.
func TestRun_CancellationStopsBetweenUsers(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	path := filepath.Join(t.TempDir(), "runlog.jsonl")
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

// TestRun_ProvisionBatchIsConcurrent proves that the UsersPerBatch creates
// in a single batch overlap in time. Each fake CreateUser waits on a shared
// barrier that only releases when `batchSize` goroutines are blocked on it;
// if the sweep were sequential, the second goroutine would never reach the
// barrier (the first call would deadlock on it), and the test would time
// out. With pipelining, all N concurrent goroutines reach the barrier and
// it releases.
func TestRun_ProvisionBatchIsConcurrent(t *testing.T) {
	t.Parallel()

	const batchSize = 4
	owner := solana.NewWallet().PublicKey()

	// Custom executor: CreateUser blocks until `batchSize` goroutines are
	// in flight, then all return. DeleteUser is a no-op for this test.
	type barrierExec struct {
		mu       sync.Mutex
		inFlight int
		ch       chan struct{}
		createN  atomic.Int32
		created  []serviceability.User
		ownerPub solana.PublicKey
	}
	be := &barrierExec{ch: make(chan struct{}), ownerPub: owner}
	create := func(ctx context.Context, idx int) (sweep.CreateResult, error) {
		be.mu.Lock()
		be.inFlight++
		if be.inFlight == batchSize {
			close(be.ch) // release everyone
		}
		ch := be.ch
		be.mu.Unlock()
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			return sweep.CreateResult{}, fmt.Errorf("idx=%d timed out waiting for batch peers — sweep is not running concurrently", idx)
		}
		be.createN.Add(1)
		var pk solana.PublicKey
		pk[0] = byte(idx)
		pk[31] = 0xAA
		be.mu.Lock()
		be.created = append(be.created, serviceability.User{Owner: be.ownerPub, ClientIp: [4]byte{100, 0, 0, byte(idx + 1)}, PubKey: pk})
		be.mu.Unlock()
		now := time.Now()
		return sweep.CreateResult{UserPDA: pk, TunnelID: uint16(500 + idx), ConfirmedAt: now, ActivatedAt: now}, nil
	}
	// Lambda-style Executor adapter so we don't need a new struct.
	exec := &funcExecutor{
		list: func(ctx context.Context) ([]serviceability.User, error) {
			be.mu.Lock()
			defer be.mu.Unlock()
			out := make([]serviceability.User, len(be.created))
			copy(out, be.created)
			return out, nil
		},
		create: create,
		delete: func(ctx context.Context, pk solana.PublicKey) (sweep.DeleteResult, error) {
			now := time.Now()
			return sweep.DeleteResult{ConfirmedAt: now, ActivatedAt: now}, nil
		},
	}

	path := filepath.Join(t.TempDir(), "runlog.jsonl")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	cfg := sweep.Config{
		RunID:         "run-concurrent",
		Target:        batchSize,
		UsersPerBatch: batchSize, // one batch of all four
		Hold:          0,
		OwnerFilter:   owner,
		Executor:      exec,
		Agent:         agent.NewNoop(nil),
		Runlog:        w,
		Clock:         &fakeClock{now: time.Unix(1, 0)},
	}
	require.NoError(t, sweep.Run(context.Background(), cfg))
	assert.Equal(t, int32(batchSize), be.createN.Load())
}

// funcExecutor lets a test build a sweep.Executor from three function values
// without declaring a new struct each time.
type funcExecutor struct {
	list   func(ctx context.Context) ([]serviceability.User, error)
	create func(ctx context.Context, idx int) (sweep.CreateResult, error)
	delete func(ctx context.Context, pk solana.PublicKey) (sweep.DeleteResult, error)
}

func (e *funcExecutor) ListUsers(ctx context.Context) ([]serviceability.User, error) {
	return e.list(ctx)
}
func (e *funcExecutor) CreateUser(ctx context.Context, idx int) (sweep.CreateResult, error) {
	return e.create(ctx, idx)
}
func (e *funcExecutor) DeleteUser(ctx context.Context, pk solana.PublicKey) (sweep.DeleteResult, error) {
	return e.delete(ctx, pk)
}

// TestRun_WaitsForAgentQuiescenceAfterDeprovision exercises the post-deprovision
// quiescence wait: after deprovision returns, Run must block until the agent
// has been silent (no EventApplied) for AgentQuietWindow before cancelling the
// SSH session. The test emits an Applied during deprovision, then verifies Run
// takes at least AgentQuietWindow to return after deprovision's gate closes.
//
// Uses a real clock — the wait uses cfg.Clock.After(time.Second) for tick
// pacing, so a fake clock would either fire instantly (deadlock-free but
// untestable) or never (deadlock). With AgentQuietWindow = 100 ms and a
// real clock the test bounds at ~150 ms wallclock.
func TestRun_WaitsForAgentQuiescenceAfterDeprovision(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	// Block deprovision so we can emit an Applied while users are still
	// registered, then watch how long Run takes to return after the gate
	// releases.
	gate := make(chan struct{})
	exec.deleteGate = gate

	ag := newScriptedAgent()

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	const quietWindow = 100 * time.Millisecond
	cfg := sweep.Config{
		RunID:                  "run-quiesce",
		Target:                 1,
		UsersPerBatch:          1,
		Hold:                   0,
		AgentQuietWindow:       quietWindow,
		AgentQuiescenceTimeout: 5 * time.Second,
		OwnerFilter:            owner,
		Executor:               exec,
		Agent:                  ag,
		Runlog:                 w,
		Clock:                  sweep.RealClock{},
	}
	done := make(chan error, 1)
	go func() { done <- sweep.Run(context.Background(), cfg) }()

	deadline := time.Now().Add(time.Second)
	for exec.deleteN.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("sweep did not reach deprovision within 1s")
		}
		time.Sleep(time.Millisecond)
	}
	// Mark the agent as active right before releasing deprovision so the
	// quiescence wait sees a recent Applied.
	ag.Emit(agent.Event{Kind: agent.EventApplied, TunnelID: 500, At: time.Now()})

	gateReleased := time.Now()
	close(gate)

	select {
	case runErr := <-done:
		require.NoError(t, runErr)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of gate release")
	}
	elapsed := time.Since(gateReleased)
	// Wait should have blocked for at least roughly the quiet window. Allow a
	// generous lower bound (quietWindow/2) to absorb scheduler jitter; the
	// negative case (wait skipped entirely) returns in < 10 ms and would
	// still trip a quietWindow/2 floor.
	assert.GreaterOrEqual(t, elapsed, quietWindow/2,
		"Run returned %v after gate release; expected ≥ %v (quiet window)", elapsed, quietWindow/2)
}

// TestRun_SkipsQuiescenceWaitWhenNoAppliedObserved confirms the wait is a
// no-op when the agent never emitted an Applied — common in --no-agent runs
// and on early-failure paths where teardown shouldn't pay the wait cost.
func TestRun_SkipsQuiescenceWaitWhenNoAppliedObserved(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	cfg := sweep.Config{
		RunID:                  "run-quiesce-skip",
		Target:                 1,
		UsersPerBatch:          1,
		Hold:                   0,
		AgentQuietWindow:       30 * time.Second, // would dominate run time if the wait did NOT skip
		AgentQuiescenceTimeout: 60 * time.Second,
		OwnerFilter:            owner,
		Executor:               exec,
		Agent:                  agent.NewNoop(nil),
		Runlog:                 w,
		Clock:                  sweep.RealClock{},
	}

	start := time.Now()
	require.NoError(t, sweep.Run(context.Background(), cfg))
	elapsed := time.Since(start)
	assert.Less(t, elapsed, time.Second,
		"Run took %v with a 30s quiet window but no Applied events — wait should have skipped", elapsed)
}
