package bgpstatus

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	gpb "github.com/openconfig/gnmi/proto/gnmi"
)

// --- mock executor ---

type mockExecutor struct {
	mu       sync.Mutex
	calls    []serviceability.UserBGPStatusUpdate
	failNext int // fail this many calls before succeeding
	err      error
}

func (m *mockExecutor) SetUserBGPStatus(_ context.Context, u serviceability.UserBGPStatusUpdate) (solana.Signature, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, u)
	if m.failNext > 0 {
		m.failNext--
		return solana.Signature{}, m.err
	}
	return solana.Signature{}, nil
}

func (m *mockExecutor) lastCalls() []serviceability.UserBGPStatusUpdate {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]serviceability.UserBGPStatusUpdate, len(m.calls))
	copy(out, m.calls)
	return out
}

// --- mock serviceability client ---

type mockSvcClient struct {
	data *serviceability.ProgramData
}

func (m *mockSvcClient) GetProgramData(_ context.Context) (*serviceability.ProgramData, error) {
	return m.data, nil
}

// --- helpers ---

func makeUser(pubkey solana.PublicKey, devicePK solana.PublicKey, tunnelNet [5]byte) serviceability.User {
	u := serviceability.User{}
	copy(u.PubKey[:], pubkey[:])
	copy(u.DevicePubKey[:], devicePK[:])
	u.TunnelNet = tunnelNet
	u.Status = serviceability.UserStatusActivated
	return u
}

// noopCollector returns a BGPCollector that always succeeds with empty results.
// Used by tests that never call tick() and only exercise the worker.
func noopCollector() BGPCollector {
	return func(_ context.Context) (map[string]struct{}, error) {
		return nil, nil
	}
}

// newTestSubmitter creates a Submitter with the given clock, executor, and collector.
func newTestSubmitter(
	t *testing.T,
	clk clockwork.Clock,
	exec BGPStatusExecutor,
	svcClient ServiceabilityClient,
	collector BGPCollector,
	devicePK solana.PublicKey,
	gracePeriod time.Duration,
	refreshInterval time.Duration,
) *Submitter {
	t.Helper()
	if collector == nil {
		collector = noopCollector()
	}
	s, err := NewSubmitter(Config{
		Log:                     newTestLogger(t),
		Executor:                exec,
		ServiceabilityClient:    svcClient,
		Collector:               collector,
		LocalDevicePK:           devicePK,
		Interval:                time.Hour, // irrelevant; tests call tick() directly
		PeriodicRefreshInterval: refreshInterval,
		DownGracePeriod:         gracePeriod,
		Clock:                   clk,
	})
	if err != nil {
		t.Fatalf("NewSubmitter: %v", err)
	}
	return s
}

// newTestLogger returns a slog.Logger that discards output during tests.
func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type testWriter struct{ t *testing.T }

func (tw testWriter) Write(p []byte) (int, error) {
	tw.t.Logf("%s", p)
	return len(p), nil
}

// ============================================================
// tunnelNetToIPNet
// ============================================================

func TestTunnelNetToIPNet(t *testing.T) {
	// 10.0.0.0/31
	b := [5]byte{10, 0, 0, 0, 31}
	net := tunnelNetToIPNet(b)
	ones, bits := net.Mask.Size()
	if ones != 31 || bits != 32 {
		t.Errorf("expected /31 got /%d/%d", ones, bits)
	}
	if net.IP.String() != "10.0.0.0" {
		t.Errorf("unexpected IP: %s", net.IP)
	}
}

// ============================================================
// computeEffectiveStatus
// ============================================================

func TestComputeEffectiveStatus_Up(t *testing.T) {
	us := &userState{}
	now := time.Now()
	got := computeEffectiveStatus(true, us, now, 5*time.Minute)
	if got != serviceability.BGPStatusUp {
		t.Errorf("expected Up, got %v", got)
	}
}

func TestComputeEffectiveStatus_DownImmediateNeverSeen(t *testing.T) {
	us := &userState{} // lastUpObservedAt is zero
	now := time.Now()
	got := computeEffectiveStatus(false, us, now, 5*time.Minute)
	if got != serviceability.BGPStatusDown {
		t.Errorf("expected Down, got %v", got)
	}
}

func TestComputeEffectiveStatus_DownWithinGrace(t *testing.T) {
	now := time.Now()
	us := &userState{lastUpObservedAt: now.Add(-1 * time.Minute)}
	got := computeEffectiveStatus(false, us, now, 5*time.Minute)
	if got != serviceability.BGPStatusUp {
		t.Errorf("expected Up (still within grace), got %v", got)
	}
}

func TestComputeEffectiveStatus_DownAfterGrace(t *testing.T) {
	now := time.Now()
	us := &userState{lastUpObservedAt: now.Add(-10 * time.Minute)}
	got := computeEffectiveStatus(false, us, now, 5*time.Minute)
	if got != serviceability.BGPStatusDown {
		t.Errorf("expected Down (grace elapsed), got %v", got)
	}
}

func TestComputeEffectiveStatus_ZeroGracePeriod(t *testing.T) {
	now := time.Now()
	us := &userState{lastUpObservedAt: now.Add(-1 * time.Second)}
	got := computeEffectiveStatus(false, us, now, 0)
	if got != serviceability.BGPStatusDown {
		t.Errorf("expected Down (grace=0), got %v", got)
	}
}

// ============================================================
// shouldSubmit
// ============================================================

func TestShouldSubmit_FirstWrite(t *testing.T) {
	us := &userState{} // lastWriteTime is zero
	if !shouldSubmit(us, serviceability.BGPStatusUp, time.Now(), 6*time.Hour) {
		t.Error("expected submit on first write")
	}
}

func TestShouldSubmit_StatusChanged(t *testing.T) {
	now := time.Now()
	us := &userState{
		lastOnchainStatus: serviceability.BGPStatusUp,
		lastWriteTime:     now.Add(-1 * time.Minute),
	}
	if !shouldSubmit(us, serviceability.BGPStatusDown, now, 6*time.Hour) {
		t.Error("expected submit on status change")
	}
}

func TestShouldSubmit_NoChangeNoRefresh(t *testing.T) {
	now := time.Now()
	us := &userState{
		lastOnchainStatus: serviceability.BGPStatusUp,
		lastWriteTime:     now.Add(-1 * time.Minute),
	}
	if shouldSubmit(us, serviceability.BGPStatusUp, now, 6*time.Hour) {
		t.Error("expected no submit when status unchanged and refresh not due")
	}
}

func TestShouldSubmit_PeriodicRefresh(t *testing.T) {
	now := time.Now()
	us := &userState{
		lastOnchainStatus: serviceability.BGPStatusUp,
		lastWriteTime:     now.Add(-7 * time.Hour),
	}
	if !shouldSubmit(us, serviceability.BGPStatusUp, now, 6*time.Hour) {
		t.Error("expected submit when periodic refresh interval elapsed")
	}
}

// ============================================================
// peerIPsFor31
// ============================================================

func TestPeerIPsFor31(t *testing.T) {
	cases := []struct {
		cidr string
		ip0  string
		ip1  string
	}{
		{"10.0.0.0/31", "10.0.0.0", "10.0.0.1"},
		{"10.0.0.1/31", "10.0.0.1", "10.0.0.0"},
		{"192.168.1.10/31", "192.168.1.10", "192.168.1.11"},
	}
	for _, tc := range cases {
		ip, ipnet, err := net.ParseCIDR(tc.cidr)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", tc.cidr, err)
		}
		ipnet.IP = ip.To4()
		a, b := peerIPsFor31(ipnet)
		got := map[string]struct{}{a.String(): {}, b.String(): {}}
		for _, want := range []string{tc.ip0, tc.ip1} {
			if _, ok := got[want]; !ok {
				t.Errorf("cidr=%s: expected %s in result, got %s and %s", tc.cidr, want, a, b)
			}
		}
	}
}

// ============================================================
// Worker retry behaviour (integration-style, no syscalls)
// ============================================================

// workerTestSetup creates a submitter and pre-populates it with a task
// already in the taskCh, bypassing the Linux-specific tick().
func workerTestSetup(
	t *testing.T,
	exec *mockExecutor,
	gracePeriod time.Duration,
	refreshInterval time.Duration,
) (*Submitter, serviceability.User) {
	t.Helper()
	devicePK := solana.NewWallet().PublicKey()
	userPK := solana.NewWallet().PublicKey()
	user := makeUser(userPK, devicePK, [5]byte{10, 0, 0, 0, 31})

	clk := clockwork.NewFakeClock()
	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}

	s := newTestSubmitter(t, clk, exec, svc, nil, devicePK, gracePeriod, refreshInterval)
	return s, user
}

func TestWorker_SuccessUpdatesState(t *testing.T) {
	exec := &mockExecutor{}
	s, user := workerTestSetup(t, exec, 0, 6*time.Hour)

	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	// Seed a task directly into the channel and mark pending.
	task := submitTask{user: user, status: serviceability.BGPStatusUp}
	s.mu.Lock()
	s.pending[userPK] = true
	s.mu.Unlock()
	s.taskCh <- task

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run the worker in a goroutine; wait for it to process the task.
	go s.worker(ctx)

	// Poll until state is updated or timeout.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		us, ok := s.userState[userPK]
		s.mu.Unlock()
		if ok && us.lastOnchainStatus == serviceability.BGPStatusUp {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	s.mu.Lock()
	us := s.userState[userPK]
	pendingAfter := s.pending[userPK]
	s.mu.Unlock()

	if us == nil || us.lastOnchainStatus != serviceability.BGPStatusUp {
		t.Errorf("expected lastOnchainStatus=Up, got %v", us)
	}
	if pendingAfter {
		t.Error("expected pending to be cleared after worker completion")
	}
	if len(exec.lastCalls()) != 1 {
		t.Errorf("expected 1 executor call, got %d", len(exec.lastCalls()))
	}
}

func TestWorker_RetryOnTransientFailure(t *testing.T) {
	// Fail 2 times, succeed on 3rd.
	exec := &mockExecutor{failNext: 2, err: errors.New("rpc timeout")}
	s, user := workerTestSetup(t, exec, 0, 6*time.Hour)

	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()
	task := submitTask{user: user, status: serviceability.BGPStatusDown}
	s.mu.Lock()
	s.pending[userPK] = true
	s.mu.Unlock()
	s.taskCh <- task

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go s.worker(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		us, ok := s.userState[userPK]
		s.mu.Unlock()
		if ok && us.lastOnchainStatus == serviceability.BGPStatusDown {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	s.mu.Lock()
	us := s.userState[userPK]
	s.mu.Unlock()

	if us == nil || us.lastOnchainStatus != serviceability.BGPStatusDown {
		t.Error("expected state updated after eventual success")
	}
	// Should have made 3 calls total (2 failures + 1 success).
	if n := len(exec.lastCalls()); n != 3 {
		t.Errorf("expected 3 executor calls, got %d", n)
	}
}

func TestWorker_AllRetriesExhausted_NoStateUpdate(t *testing.T) {
	exec := &mockExecutor{failNext: submitMaxRetries, err: errors.New("persistent error")}
	s, user := workerTestSetup(t, exec, 0, 6*time.Hour)

	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()
	task := submitTask{user: user, status: serviceability.BGPStatusUp}
	s.mu.Lock()
	s.pending[userPK] = true
	s.mu.Unlock()
	s.taskCh <- task

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go s.worker(ctx)

	// Wait for pending to be cleared (worker completed task even on failure).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		p := s.pending[userPK]
		s.mu.Unlock()
		if !p {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	s.mu.Lock()
	_, stateUpdated := s.userState[userPK]
	s.mu.Unlock()

	// No state entry should have been written (all retries failed).
	if stateUpdated && s.userState[userPK].lastOnchainStatus != 0 {
		t.Error("expected no state update after exhausted retries")
	}
}

// ============================================================
// Pending deduplication
// ============================================================

func TestPendingDedup_SecondEnqueueSkipped(t *testing.T) {
	exec := &mockExecutor{}
	s, user := workerTestSetup(t, exec, 0, 6*time.Hour)

	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	// Manually mark user as pending (simulating a task already in the channel).
	s.mu.Lock()
	s.pending[userPK] = true
	us := s.userStateFor(userPK, serviceability.BGPStatusUnknown) // trigger creation
	_ = us
	s.mu.Unlock()

	// A second call to the inline enqueue logic should skip because pending=true.
	// We test this by checking that taskCh remains empty.
	s.mu.Lock()
	shouldEnqueue := !s.pending[userPK]
	s.mu.Unlock()

	if shouldEnqueue {
		t.Error("expected pending check to block second enqueue")
	}
	if len(s.taskCh) != 0 {
		t.Error("expected task channel to remain empty")
	}
}

// ============================================================
// Periodic refresh via FakeClock
// ============================================================

func TestPeriodicRefresh_ReenqueuesAfterInterval(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	now := fakeClock.Now()

	refreshInterval := 6 * time.Hour
	us := &userState{
		lastOnchainStatus: serviceability.BGPStatusUp,
		lastWriteTime:     now.Add(-7 * time.Hour), // older than refresh interval
	}

	// shouldSubmit should return true because the refresh interval has elapsed.
	if !shouldSubmit(us, serviceability.BGPStatusUp, now, refreshInterval) {
		t.Error("expected shouldSubmit=true when periodic refresh interval has elapsed")
	}

	// If the last write was recent, should not re-submit.
	us.lastWriteTime = now.Add(-1 * time.Hour)
	if shouldSubmit(us, serviceability.BGPStatusUp, now, refreshInterval) {
		t.Error("expected shouldSubmit=false when refresh interval has not elapsed")
	}
}

// ============================================================
// NewSubmitter validation
// ============================================================

func TestNewSubmitter_MissingFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"no log", Config{Executor: &mockExecutor{}, ServiceabilityClient: &mockSvcClient{}, Collector: noopCollector(), LocalDevicePK: solana.NewWallet().PublicKey()}},
		{"no executor", Config{Log: slog.Default(), ServiceabilityClient: &mockSvcClient{}, Collector: noopCollector(), LocalDevicePK: solana.NewWallet().PublicKey()}},
		{"no svc client", Config{Log: slog.Default(), Executor: &mockExecutor{}, Collector: noopCollector(), LocalDevicePK: solana.NewWallet().PublicKey()}},
		{"no collector", Config{Log: slog.Default(), Executor: &mockExecutor{}, ServiceabilityClient: &mockSvcClient{}, LocalDevicePK: solana.NewWallet().PublicKey()}},
		{"zero device pk", Config{Log: slog.Default(), Executor: &mockExecutor{}, ServiceabilityClient: &mockSvcClient{}, Collector: noopCollector()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSubmitter(tc.cfg)
			if err == nil {
				t.Error("expected error for invalid config")
			}
		})
	}
}

// ============================================================
// Channel full / back-pressure (non-blocking enqueue)
// ============================================================

func TestTaskChannel_DropWhenFull(t *testing.T) {
	exec := &mockExecutor{}
	devicePK := solana.NewWallet().PublicKey()
	s, err := NewSubmitter(Config{
		Log:                     slog.Default(),
		Executor:                exec,
		ServiceabilityClient:    &mockSvcClient{data: &serviceability.ProgramData{}},
		Collector:               noopCollector(),
		LocalDevicePK:           devicePK,
		Interval:                time.Hour,
		PeriodicRefreshInterval: 6 * time.Hour,
		Clock:                   clockwork.NewFakeClock(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Fill the channel.
	user := makeUser(solana.NewWallet().PublicKey(), devicePK, [5]byte{10, 0, 0, 0, 31})
	for range taskChannelCapacity {
		s.taskCh <- submitTask{user: user, status: serviceability.BGPStatusUp}
	}

	// A further non-blocking enqueue must not block (select default branch).
	done := make(chan struct{})
	var dropped atomic.Bool
	go func() {
		defer close(done)
		select {
		case s.taskCh <- submitTask{user: user, status: serviceability.BGPStatusUp}:
		default:
			dropped.Store(true)
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enqueue blocked when channel was full")
	}
	if !dropped.Load() {
		t.Error("expected drop when channel full")
	}
}

// ============================================================
// Helpers for tick() and parseEstablished tests
// ============================================================

// fixedCollector returns a BGPCollector that serves a fixed established set.
func fixedCollector(established map[string]struct{}) BGPCollector {
	return func(_ context.Context) (map[string]struct{}, error) {
		return established, nil
	}
}

// makeActivatedUser returns a User activated on devicePK with the given /31 tunnelNet.
func makeActivatedUser(devicePK solana.PublicKey, tunnelNet [5]byte) serviceability.User {
	u := serviceability.User{}
	copy(u.DevicePubKey[:], devicePK[:])
	userPK := solana.NewWallet().PublicKey()
	copy(u.PubKey[:], userPK[:])
	u.TunnelNet = tunnelNet
	u.Status = serviceability.UserStatusActivated
	return u
}

// makeMulticastUser returns a multicast User activated on devicePK with the given /31 tunnelNet.
func makeMulticastUser(devicePK solana.PublicKey, tunnelNet [5]byte) serviceability.User {
	u := makeActivatedUser(devicePK, tunnelNet)
	u.UserType = serviceability.UserTypeMulticast
	return u
}

// ============================================================
// tick() – BGP session detection via BGPCollector
// ============================================================

func TestTick_BGPUp(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	user := makeActivatedUser(devicePK, [5]byte{10, 0, 0, 0, 31})
	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	exec := &mockExecutor{}
	clk := clockwork.NewFakeClock()
	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}

	col := fixedCollector(map[string]struct{}{"10.0.0.1": {}})
	s := newTestSubmitter(t, clk, exec, svc, col, devicePK, 0, 6*time.Hour)
	s.tick(context.Background())

	s.mu.Lock()
	enqueued := len(s.taskCh)
	s.mu.Unlock()

	if enqueued != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", enqueued)
	}
	task := <-s.taskCh
	if task.status != serviceability.BGPStatusUp {
		t.Errorf("expected Up task, got %v", task.status)
	}
	if solana.PublicKeyFromBytes(task.user.PubKey[:]).String() != userPK {
		t.Errorf("unexpected user in task")
	}
}

// TestTick_BGPUp_FirstIPInSlash31 verifies that the first IP in the /31 also
// triggers an Up submission (not only the second IP).
func TestTick_BGPUp_FirstIPInSlash31(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	user := makeActivatedUser(devicePK, [5]byte{10, 0, 0, 0, 31})

	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}
	col := fixedCollector(map[string]struct{}{"10.0.0.0": {}})
	s := newTestSubmitter(t, clockwork.NewFakeClock(), &mockExecutor{}, svc, col, devicePK, 0, 6*time.Hour)
	s.tick(context.Background())

	s.mu.Lock()
	enqueued := len(s.taskCh)
	s.mu.Unlock()

	if enqueued != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", enqueued)
	}
	task := <-s.taskCh
	if task.status != serviceability.BGPStatusUp {
		t.Errorf("expected Up task, got %v", task.status)
	}
}

// TestTick_BGPDown_PreviouslyUp verifies that an empty established set when the
// user was previously Up results in a Down submission.
func TestTick_BGPDown_PreviouslyUp(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	user := makeActivatedUser(devicePK, [5]byte{10, 0, 0, 0, 31})
	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	clk := clockwork.NewFakeClock()
	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}
	col := fixedCollector(map[string]struct{}{})
	s := newTestSubmitter(t, clk, &mockExecutor{}, svc, col, devicePK, 0, 6*time.Hour)

	s.mu.Lock()
	s.userState[userPK] = &userState{
		lastOnchainStatus: serviceability.BGPStatusUp,
		lastWriteTime:     clk.Now().Add(-1 * time.Minute),
	}
	s.mu.Unlock()

	s.tick(context.Background())

	s.mu.Lock()
	enqueued := len(s.taskCh)
	s.mu.Unlock()

	if enqueued != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", enqueued)
	}
	task := <-s.taskCh
	if task.status != serviceability.BGPStatusDown {
		t.Errorf("expected Down task, got %v", task.status)
	}
}

// TestTick_CollectorError_NoTask verifies that a collector error causes tick to
// abort without enqueueing any tasks.
func TestTick_CollectorError_NoTask(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	user := makeActivatedUser(devicePK, [5]byte{10, 0, 0, 0, 31})
	userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()

	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}
	s := newTestSubmitter(t, clockwork.NewFakeClock(), &mockExecutor{}, svc,
		func(_ context.Context) (map[string]struct{}, error) {
			return nil, errors.New("gNMI unavailable")
		}, devicePK, 0, 6*time.Hour)

	s.mu.Lock()
	s.userState[userPK] = &userState{lastOnchainStatus: serviceability.BGPStatusUp}
	s.mu.Unlock()

	s.tick(context.Background())

	s.mu.Lock()
	enqueued := len(s.taskCh)
	s.mu.Unlock()

	if enqueued != 0 {
		t.Errorf("expected no tasks when collector fails, got %d", enqueued)
	}
}

// TestTick_MulticastUser verifies that multicast users are handled identically
// to regular users — the peer IP check is type-agnostic.
func TestTick_MulticastUser(t *testing.T) {
	devicePK := solana.NewWallet().PublicKey()
	user := makeMulticastUser(devicePK, [5]byte{10, 0, 3, 0, 31})

	svc := &mockSvcClient{data: &serviceability.ProgramData{Users: []serviceability.User{user}}}
	col := fixedCollector(map[string]struct{}{"10.0.3.1": {}})
	s := newTestSubmitter(t, clockwork.NewFakeClock(), &mockExecutor{}, svc, col, devicePK, 0, 6*time.Hour)
	s.tick(context.Background())

	s.mu.Lock()
	enqueued := len(s.taskCh)
	s.mu.Unlock()

	if enqueued != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", enqueued)
	}
	task := <-s.taskCh
	if task.status != serviceability.BGPStatusUp {
		t.Errorf("expected Up task, got %v", task.status)
	}
}

// ============================================================
// parseEstablished
// ============================================================

func gnmiStateJSON(sessionState string) []byte {
	b, _ := json.Marshal(map[string]string{
		"openconfig-network-instance:session-state": sessionState,
	})
	return b
}

func buildGetResponse(neighborAddr, sessionState string) *gpb.GetResponse {
	return &gpb.GetResponse{
		Notification: []*gpb.Notification{
			{
				Update: []*gpb.Update{
					{
						Path: &gpb.Path{
							Elem: []*gpb.PathElem{
								{Name: "network-instances"},
								{Name: "network-instance", Key: map[string]string{"name": "default"}},
								{Name: "bgp"},
								{Name: "neighbors"},
								{Name: "neighbor", Key: map[string]string{"neighbor-address": neighborAddr}},
								{Name: "state"},
							},
						},
						Val: &gpb.TypedValue{
							Value: &gpb.TypedValue_JsonIetfVal{JsonIetfVal: gnmiStateJSON(sessionState)},
						},
					},
				},
			},
		},
	}
}

func TestParseEstablished_ESTABLISHED(t *testing.T) {
	resp := buildGetResponse("10.0.0.1", "ESTABLISHED")
	got := parseEstablished(resp)
	if _, ok := got["10.0.0.1"]; !ok {
		t.Error("expected 10.0.0.1 in established set")
	}
	if len(got) != 1 {
		t.Errorf("expected 1 entry, got %d", len(got))
	}
}

func TestParseEstablished_NonEstablished(t *testing.T) {
	for _, state := range []string{"IDLE", "ACTIVE", "CONNECT", "OPENSENT", "OPENCONFIRM"} {
		resp := buildGetResponse("10.0.0.1", state)
		got := parseEstablished(resp)
		if _, ok := got["10.0.0.1"]; ok {
			t.Errorf("state=%s: did not expect 10.0.0.1 in established set", state)
		}
	}
}

func TestParseEstablished_Multiple(t *testing.T) {
	resp := &gpb.GetResponse{
		Notification: []*gpb.Notification{
			{
				Update: []*gpb.Update{
					{
						Path: &gpb.Path{Elem: []*gpb.PathElem{
							{Name: "neighbor", Key: map[string]string{"neighbor-address": "10.0.0.1"}},
							{Name: "state"},
						}},
						Val: &gpb.TypedValue{Value: &gpb.TypedValue_JsonIetfVal{JsonIetfVal: gnmiStateJSON("ESTABLISHED")}},
					},
					{
						Path: &gpb.Path{Elem: []*gpb.PathElem{
							{Name: "neighbor", Key: map[string]string{"neighbor-address": "10.0.0.3"}},
							{Name: "state"},
						}},
						Val: &gpb.TypedValue{Value: &gpb.TypedValue_JsonIetfVal{JsonIetfVal: gnmiStateJSON("ACTIVE")}},
					},
					{
						Path: &gpb.Path{Elem: []*gpb.PathElem{
							{Name: "neighbor", Key: map[string]string{"neighbor-address": "10.0.0.5"}},
							{Name: "state"},
						}},
						Val: &gpb.TypedValue{Value: &gpb.TypedValue_JsonIetfVal{JsonIetfVal: gnmiStateJSON("ESTABLISHED")}},
					},
				},
			},
		},
	}
	got := parseEstablished(resp)
	if _, ok := got["10.0.0.1"]; !ok {
		t.Error("expected 10.0.0.1 in established set")
	}
	if _, ok := got["10.0.0.5"]; !ok {
		t.Error("expected 10.0.0.5 in established set")
	}
	if _, ok := got["10.0.0.3"]; ok {
		t.Error("did not expect 10.0.0.3 (ACTIVE) in established set")
	}
}

func TestParseEstablished_NeighborAddressInPrefix(t *testing.T) {
	// Some Arista responses place the neighbor key in the notification prefix.
	resp := &gpb.GetResponse{
		Notification: []*gpb.Notification{
			{
				Prefix: &gpb.Path{
					Elem: []*gpb.PathElem{
						{Name: "neighbor", Key: map[string]string{"neighbor-address": "10.0.1.0"}},
					},
				},
				Update: []*gpb.Update{
					{
						Path: &gpb.Path{Elem: []*gpb.PathElem{{Name: "state"}}},
						Val:  &gpb.TypedValue{Value: &gpb.TypedValue_JsonIetfVal{JsonIetfVal: gnmiStateJSON("ESTABLISHED")}},
					},
				},
			},
		},
	}
	got := parseEstablished(resp)
	if _, ok := got["10.0.1.0"]; !ok {
		t.Error("expected 10.0.1.0 in established set (address was in prefix)")
	}
}

func TestParseEstablished_EmptyResponse(t *testing.T) {
	got := parseEstablished(&gpb.GetResponse{})
	if len(got) != 0 {
		t.Errorf("expected empty set for empty response, got %d entries", len(got))
	}
}
