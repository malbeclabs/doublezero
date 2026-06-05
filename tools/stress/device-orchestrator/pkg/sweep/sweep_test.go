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
		{name: "positive quiet window with zero timeout", cfg: sweep.Config{Target: 1, UsersPerBatch: 1, RunID: "r", OwnerFilter: owner, Executor: &fakeExecutor{}, Runlog: &runlog.Writer{}, AgentQuietWindow: time.Second}},
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

	// Block until the consumer goroutine has written the applied row to the
	// runlog. consumeAgentEvents calls tracker.markEvent BEFORE Runlog.Append,
	// so the row's presence proves the tracker was marked. Without this gate,
	// close(gate) can race the consumer; if the tracker is still empty when
	// waitForAgentQuiescence reads it, the wait is skipped and the test
	// flakes (~5/6 failures under the parallel suite).
	require.Eventually(t, func() bool {
		for _, r := range readRows(t, path) {
			if r.Event == runlog.EventApplied && r.TunnelID == 500 {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond, "consumer goroutine did not record applied row")

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

// TestRun_PerBatchCatchUpWaitsBetweenBatches proves that when
// ApplyPerBatchCatchUp is set, the sweep blocks after each batch
// until the agent has emitted enough Applied events to cover the
// cumulative count submitted so far — i.e. the orchestrator stops
// pre-creating batches faster than the agent can apply them. Without
// this, batch 2 would start as soon as batch 1's onchain activates
// land regardless of agent progress (the default behavior).
func TestRun_PerBatchCatchUpWaitsBetweenBatches(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	ag := newScriptedAgent()

	// Signal when batch 2 starts (the 9th CreateUser call). We use a
	// large enough batch size (8) and target (16) that batch 1's
	// cumulative target (8) sits above the catch-up grace (4), so
	// applied=0 actually blocks rather than passing-with-grace.
	const batchSize = 8
	const target = 16
	batch2Reached := make(chan struct{})
	var once sync.Once
	exec.afterCreate = func(calls int) {
		if calls == batchSize+1 {
			once.Do(func() { close(batch2Reached) })
		}
	}

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	cfg := sweep.Config{
		RunID:                "run-per-batch-catch-up",
		Target:               target,
		UsersPerBatch:        batchSize,
		Hold:                 0,
		ApplyCatchUpTimeout:  3 * time.Second,
		ApplyPerBatchCatchUp: true,
		OwnerFilter:          owner,
		Executor:             exec,
		Agent:                ag,
		Runlog:               w,
		Clock:                sweep.RealClock{},
	}
	done := make(chan error, 1)
	go func() { done <- sweep.Run(context.Background(), cfg) }()

	// Wait for batch 1 to land (batchSize create calls), then confirm
	// batch 2 hasn't started — the per-batch wait should block it.
	deadline := time.Now().Add(2 * time.Second)
	for exec.createN.Load() < batchSize {
		if time.Now().After(deadline) {
			t.Fatalf("batch 1 did not complete within 2s (got %d creates)", exec.createN.Load())
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	select {
	case <-batch2Reached:
		t.Fatal("batch 2 started before any Applied event — per-batch wait did not block")
	default:
	}

	// Emit batchSize-grace+1 Applied events to push applied+grace
	// past the cumulative target and unblock the wait.
	for i := 0; i < batchSize; i++ {
		ag.Emit(agent.Event{Kind: agent.EventApplied, TunnelID: uint16(500 + i), At: time.Now()})
	}

	select {
	case <-batch2Reached:
		// Expected.
	case <-time.After(3 * time.Second):
		t.Fatal("batch 2 did not start within 3s of Applied events")
	}

	// Let the run finish; emit enough Applieds to clear remaining waits.
	for i := 0; i < target; i++ {
		ag.Emit(agent.Event{Kind: agent.EventApplied, TunnelID: uint16(500 + batchSize + i), At: time.Now()})
	}
	select {
	case runErr := <-done:
		require.NoError(t, runErr)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s after all Applieds emitted")
	}
}

// TestRun_BlocksDeprovisionUntilAppliedCatchesUp proves the
// provision→deprovision wait blocks until enough EventApplied
// observations have landed to cover the provisioned users. Without
// this gate (hold=0) the orchestrator finishes provisioning ~40s
// ahead of the agent's slowest commit at high user counts and starts
// removing users before they've ever been added to the device.
func TestRun_BlocksDeprovisionUntilAppliedCatchesUp(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	ag := newScriptedAgent()

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	cfg := sweep.Config{
		RunID:               "run-catch-up",
		Target:              5,
		UsersPerBatch:       5,
		Hold:                0,
		ApplyCatchUpTimeout: 2 * time.Second,
		OwnerFilter:         owner,
		Executor:            exec,
		Agent:               ag,
		Runlog:              w,
		Clock:               sweep.RealClock{},
	}
	done := make(chan error, 1)
	go func() { done <- sweep.Run(context.Background(), cfg) }()

	// Wait for provision to finish (5 create calls) but no deprovision
	// yet — the catch-up wait should be blocking.
	deadline := time.Now().Add(2 * time.Second)
	for exec.createN.Load() < 5 {
		if time.Now().After(deadline) {
			t.Fatalf("provision did not reach 5 users in 2s (got %d)", exec.createN.Load())
		}
		time.Sleep(time.Millisecond)
	}
	// Give the wait a beat to enter the loop, then confirm deprovision
	// hasn't started.
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, int32(0), exec.deleteN.Load(), "deprovision started before applied caught up")

	// Emit enough Applied events to satisfy target - grace (5 - 4 = 1).
	ag.Emit(agent.Event{Kind: agent.EventApplied, TunnelID: 500, At: time.Now()})

	select {
	case runErr := <-done:
		require.NoError(t, runErr)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within 3s after Applied caught up")
	}
	assert.Equal(t, int32(5), exec.deleteN.Load(), "all 5 users should have been deprovisioned")
}

// TestRun_CatchUpWaitHonorsTimeout confirms the wait gives up and
// proceeds with deprovision when ApplyCatchUpTimeout fires, instead of
// pinning the orchestrator indefinitely on a stuck agent.
func TestRun_CatchUpWaitHonorsTimeout(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	ag := newScriptedAgent()

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	cfg := sweep.Config{
		RunID:               "run-catch-up-timeout",
		Target:              10,
		UsersPerBatch:       10,
		Hold:                0,
		ApplyCatchUpTimeout: 200 * time.Millisecond,
		OwnerFilter:         owner,
		Executor:            exec,
		Agent:               ag, // never emits Applied
		Runlog:              w,
		Clock:               sweep.RealClock{},
	}
	start := time.Now()
	require.NoError(t, sweep.Run(context.Background(), cfg))
	elapsed := time.Since(start)
	// Wait should have taken roughly the timeout. The lower bound is
	// timeout/2 (loose, absorbs scheduler jitter); the upper bound is
	// timeout * 4 (loose enough to absorb the 1s tick interval).
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond,
		"Run returned %v; expected at least the catch-up timeout floor", elapsed)
	assert.Less(t, elapsed, 5*time.Second,
		"Run took %v; expected catch-up wait to time out, not hang", elapsed)
	assert.Equal(t, int32(10), exec.deleteN.Load(),
		"deprovision should still run after the catch-up wait times out")
}

// TestRun_QuiescenceBlocksOnPendingCommit proves the wait does NOT
// declare quiescence while the agent has a config received but not
// yet committed. Without this, the wait could time out mid-diff-check
// (which can run >30s at >1MB configs) and the orchestrator would
// cancel the SSH session while the agent was still applying the
// deprovision config.
func TestRun_QuiescenceBlocksOnPendingCommit(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	// Block deprovision so we can emit a ConfigReceived during teardown
	// and observe whether the wait honors the pending-commit flag.
	gate := make(chan struct{})
	exec.deleteGate = gate

	ag := newScriptedAgent()

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	const quietWindow = 100 * time.Millisecond
	cfg := sweep.Config{
		RunID:                  "run-pending-commit",
		Target:                 1,
		UsersPerBatch:          1,
		Hold:                   0,
		AgentQuietWindow:       quietWindow,
		AgentQuiescenceTimeout: 2 * time.Second,
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

	// Emit a ConfigReceived to set the pending-commit flag, then go
	// silent. The wait MUST NOT declare quiescence until the matching
	// EventCommit lands — even after multiple quietWindow durations
	// have elapsed.
	ag.Emit(agent.Event{Kind: agent.EventConfigReceived, TunnelID: 0, At: time.Now()})

	close(gate)

	// Stay quiet for several quiet windows; the wait must still be
	// blocked because the pending-commit flag is sticky.
	time.Sleep(5 * quietWindow)
	select {
	case <-done:
		t.Fatal("Run returned while a config was pending commit — quiescence wait should block")
	default:
	}

	// Emit the matching commit; only now should the wait complete.
	ag.Emit(agent.Event{Kind: agent.EventCommit, TunnelID: 0, At: time.Now()})

	select {
	case runErr := <-done:
		require.NoError(t, runErr)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of EventCommit clearing the pending flag")
	}
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

// TestRun_WaitsForAgentQuiescenceEvenOnAbort proves that the quiescence
// wait engages and blocks for the full window when provision was
// cancelled by ctx (e.g. the observer's abort sentinel). The new branch
// (introduced together with the elapsed-since-wait-start floor) needs
// to be observable: the test asserts that the run blocks at least
// `quietWindow` after deprovision returns, even though the agent emits
// no events after ctx-cancellation. A regression that reverts either
// (a) the `err == nil` guard removal or (b) the elapsed-since-wait-start
// floor would surface here:
//   - reverting (a) → wait is skipped, run returns in well under
//     quietWindow.
//   - reverting (b) → the absolute "silent for quietWindow" predicate
//     is satisfied immediately (agent was already silent at wait
//     start), so the wait returns instantly even with the new branch.
func TestRun_WaitsForAgentQuiescenceEvenOnAbort(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	exec := newFakeExecutor(owner)
	ag := newScriptedAgent()

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.json")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	// Cancel ctx after the first user is created so provision exits with
	// context.Canceled on the next loop iteration. Deprovision still
	// runs to completion (uses WithoutCancel internally), and the wait
	// should engage despite the cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Emit the synthetic agent event well before cancellation so the
	// tracker's `lastEvent` timestamp is meaningfully older than the
	// wait's start. This forces the wait to rely on the elapsed-since-
	// wait-start floor — the absolute "silent for quietWindow" predicate
	// would otherwise be satisfied immediately.
	ag.Emit(agent.Event{Kind: agent.EventCommit, TunnelID: 0, At: time.Now()})
	exec.afterCreate = func(calls int) {
		if calls == 1 {
			cancel()
		}
	}

	const quietWindow = 200 * time.Millisecond
	cfg := sweep.Config{
		RunID:                  "run-quiesce-abort",
		Target:                 4,
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
	start := time.Now()
	go func() { done <- sweep.Run(ctx, cfg) }()

	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
	}
	elapsed := time.Since(start)
	require.Error(t, runErr, "Run should surface the ctx-cancellation error")
	assert.ErrorIs(t, runErr, context.Canceled)
	// The wait must block at least quietWindow even though ctx is
	// cancelled — that's the whole point of the abort-path branch.
	// Allow a generous lower bound (quietWindow/2) to absorb scheduler
	// jitter; the negative case (wait skipped) returns in well under
	// 10 ms.
	assert.GreaterOrEqual(t, elapsed, quietWindow/2,
		"Run returned in %v under abort; expected ≥ %v (quiet window floor)", elapsed, quietWindow/2)
	// Deprovision should still complete onchain regardless of the wait.
	rows := readRows(t, path)
	var sawDeprovisionActivate bool
	for _, r := range rows {
		if r.Event == runlog.EventDeprovisionActivate {
			sawDeprovisionActivate = true
			break
		}
	}
	assert.True(t, sawDeprovisionActivate, "deprovision should still complete onchain after abort")
}
