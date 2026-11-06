package controller

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc/peer"
)

// logEntry captures a single log call for test assertions.
type logEntry struct {
	Level   slog.Level
	Message string
}

// capturingHandler is a slog.Handler that records log entries.
type capturingHandler struct {
	mu      sync.Mutex
	entries []logEntry
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, logEntry{Level: r.Level, Message: r.Message})
	return nil
}

func (h *capturingHandler) snapshot() []logEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]logEntry(nil), h.entries...)
}
func (h *capturingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(_ string) slog.Handler      { return h }

// fakeBatch implements driver.Batch for the methods the writer uses; the
// embedded nil interface panics on anything else.
type fakeBatch struct {
	driver.Batch
	rows      [][]any
	appendErr error
	sendErr   error
	sent      bool
}

func (b *fakeBatch) Append(v ...any) error {
	if b.appendErr != nil {
		return b.appendErr
	}
	b.rows = append(b.rows, v)
	return nil
}
func (b *fakeBatch) Send() error {
	if b.sendErr != nil {
		return b.sendErr
	}
	b.sent = true
	return nil
}
func (b *fakeBatch) Close() error { return nil }

// fakeConn implements driver.Conn for the methods the writer uses. mu guards
// every field, since the schema retry loop runs on its own goroutine.
type fakeConn struct {
	driver.Conn
	mu         sync.Mutex
	prepared   []string
	batches    []*fakeBatch
	prepareErr error
	appendErr  error
	sendErr    error
	execErrOn  string // substring of a query whose Exec should fail
	execErr    error
	execs      []string
	// hasCallerIP is what the system.columns lookup for caller_ip reports.
	hasCallerIP bool
	queryErr    error
	// queries and queryArgs record each QueryRow call.
	queries   []string
	queryArgs [][]any
}

// set changes the fake's behaviour under its lock.
func (c *fakeConn) set(f func(c *fakeConn)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f(c)
}

func (c *fakeConn) execsSnapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.execs...)
}

// fakeRow implements driver.Row for the count() lookup the writer runs.
type fakeRow struct {
	driver.Row
	n   uint64
	err error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*uint64) = r.n
	return nil
}

func (c *fakeConn) QueryRow(_ context.Context, query string, args ...any) driver.Row {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queries = append(c.queries, query)
	c.queryArgs = append(c.queryArgs, args)
	if c.hasCallerIP {
		return &fakeRow{n: 1, err: c.queryErr}
	}
	return &fakeRow{err: c.queryErr}
}

func (c *fakeConn) PrepareBatch(_ context.Context, query string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prepared = append(c.prepared, query)
	if c.prepareErr != nil {
		return nil, c.prepareErr
	}
	b := &fakeBatch{appendErr: c.appendErr, sendErr: c.sendErr}
	c.batches = append(c.batches, b)
	return b, nil
}

func (c *fakeConn) Exec(_ context.Context, query string, _ ...any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.execs = append(c.execs, query)
	if c.execErrOn != "" && strings.Contains(query, c.execErrOn) {
		return c.execErr
	}
	return nil
}

func newTestWriter(conn clickhouse.Conn) (*ClickhouseWriter, *capturingHandler) {
	h := &capturingHandler{}
	return &ClickhouseWriter{
		conn:            conn,
		log:             slog.New(h),
		db:              "testdb",
		build:           Build{Version: "v1.2.3", Commit: "abc123", Date: "2026-01-01"},
		eventsEnabled:   true,
		callerIPEnabled: true,
		versionsEnabled: true,
	}, h
}

// newSchemaTestWriter returns a writer as NewClickhouseWriter does, with
// every write off until CreateTables enables it, and fast schema retries.
func newSchemaTestWriter(conn clickhouse.Conn) (*ClickhouseWriter, *capturingHandler) {
	cw, h := newTestWriter(conn)
	cw.eventsEnabled, cw.callerIPEnabled, cw.versionsEnabled = false, false, false
	cw.schemaRetryMin, cw.schemaRetryMax = time.Millisecond, 4*time.Millisecond
	return cw, h
}

// errAddCallerIP is the message logged when caller_ip cannot be added.
const errAddCallerIP = "error adding caller_ip to getconfig table, writing getconfig events without it"

// enabled reads the three write flags under the writer's lock.
func (cw *ClickhouseWriter) enabled() (events, callerIP, versions bool) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.eventsEnabled, cw.callerIPEnabled, cw.versionsEnabled
}

func writeGauge(write string) float64 {
	return testutil.ToFloat64(clickhouseWriteEnabled.WithLabelValues(write))
}

func TestFlushSuccessResetsCounterAndPreservesColumnOrder(t *testing.T) {
	conn := &fakeConn{}
	cw, _ := newTestWriter(conn)
	cw.consecutiveErrors = 2

	ts := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	cw.Record(getConfigEvent{Timestamp: ts, DevicePubkey: "dev1", CallerIP: "203.0.113.7"})
	cw.RecordVersion(agentVersionReport{
		DevicePubkey: "dev1",
		UpdatedAt:    ts,
		AgentVersion: "v0.9.0",
		AgentCommit:  "deadbeef",
		AgentDate:    "2026-07-01",
	})
	cw.flush(context.Background())

	if cw.consecutiveErrors != 0 {
		t.Errorf("consecutiveErrors = %d after successful flush, want 0", cw.consecutiveErrors)
	}
	if len(conn.batches) != 2 {
		t.Fatalf("prepared %d batches, want 2", len(conn.batches))
	}

	// Event row values must line up with the INSERT column list.
	if want := `(timestamp, device_pubkey, caller_ip)`; !strings.Contains(conn.prepared[0], want) {
		t.Fatalf("events INSERT = %q, want column list %q", conn.prepared[0], want)
	}
	if rows := conn.batches[0].rows; len(rows) != 1 || len(rows[0]) != 3 || rows[0][0] != ts || rows[0][1] != "dev1" || rows[0][2] != "203.0.113.7" {
		t.Fatalf("events batch rows = %v, want [[%v dev1 203.0.113.7]]", conn.batches[0].rows, ts)
	}

	// Version row values must line up with the INSERT column list.
	wantColumns := `(device_pubkey, updated_at, agent_version, agent_commit, agent_date, controller_version, controller_commit, controller_date)`
	if !strings.Contains(conn.prepared[1], wantColumns) {
		t.Fatalf("versions INSERT = %q, want column list %q", conn.prepared[1], wantColumns)
	}
	rows := conn.batches[1].rows
	if len(rows) != 1 {
		t.Fatalf("versions batch has %d rows, want 1", len(rows))
	}
	want := []any{"dev1", ts, "v0.9.0", "deadbeef", "2026-07-01", "v1.2.3", "abc123", "2026-01-01"}
	if len(rows[0]) != len(want) {
		t.Fatalf("versions row has %d values, want %d", len(rows[0]), len(want))
	}
	for i, v := range want {
		if rows[0][i] != v {
			t.Errorf("versions row[%d] = %v, want %v", i, rows[0][i], v)
		}
	}
}

func TestFlushNoopTickDoesNotResetCounter(t *testing.T) {
	conn := &fakeConn{}
	cw, _ := newTestWriter(conn)
	cw.consecutiveErrors = 2

	cw.flush(context.Background())

	if cw.consecutiveErrors != 2 {
		t.Errorf("consecutiveErrors = %d after no-op tick, want 2", cw.consecutiveErrors)
	}
	if len(conn.prepared) != 0 {
		t.Errorf("no-op tick prepared %d batches, want 0", len(conn.prepared))
	}
}

func TestFlushFailureIncrementsCounterOncePerTick(t *testing.T) {
	conn := &fakeConn{sendErr: errors.New("connection reset")}
	cw, h := newTestWriter(conn)

	record := func() {
		cw.Record(getConfigEvent{Timestamp: time.Time{}, DevicePubkey: "dev1"})
		cw.RecordVersion(agentVersionReport{DevicePubkey: "dev1", AgentVersion: "v0.9.0"})
	}

	// Both sub-flushes fail each tick, but the counter must advance once per
	// tick so the WARN→ERROR threshold counts failed ticks.
	for tick := 1; tick <= consecutiveErrorThreshold; tick++ {
		record()
		cw.flush(context.Background())
		if cw.consecutiveErrors != tick {
			t.Fatalf("tick %d: consecutiveErrors = %d, want %d", tick, cw.consecutiveErrors, tick)
		}
	}

	var warns, errs int
	for _, e := range h.entries {
		switch e.Level {
		case slog.LevelWarn:
			warns++
		case slog.LevelError:
			errs++
		}
	}
	// Two failing sub-flushes per tick: WARN for the first threshold-1 ticks,
	// ERROR from the threshold-th tick on.
	if wantWarns := 2 * (consecutiveErrorThreshold - 1); warns != wantWarns {
		t.Errorf("got %d WARN entries, want %d", warns, wantWarns)
	}
	if errs != 2 {
		t.Errorf("got %d ERROR entries, want 2", errs)
	}

	// A successful flush resets the counter and de-escalates back to WARN.
	conn.sendErr = nil
	record()
	cw.flush(context.Background())
	if cw.consecutiveErrors != 0 {
		t.Fatalf("consecutiveErrors = %d after successful flush, want 0", cw.consecutiveErrors)
	}
	h.entries = nil
	conn.sendErr = errors.New("connection reset")
	record()
	cw.flush(context.Background())
	for _, e := range h.entries {
		if e.Level != slog.LevelWarn {
			t.Errorf("after reset: got level %v, want WARN", e.Level)
		}
	}
}

func TestFlushAbortsBatchOnAppendError(t *testing.T) {
	conn := &fakeConn{appendErr: errors.New("wrong column count")}
	cw, _ := newTestWriter(conn)

	cw.RecordVersion(agentVersionReport{DevicePubkey: "dev1", AgentVersion: "v0.9.0"})
	cw.flush(context.Background())

	if cw.consecutiveErrors != 1 {
		t.Errorf("consecutiveErrors = %d after append error, want 1", cw.consecutiveErrors)
	}
	if len(conn.batches) != 1 {
		t.Fatalf("prepared %d batches, want 1", len(conn.batches))
	}
	if conn.batches[0].sent {
		t.Error("partial batch was sent after append error, want aborted")
	}
}

func TestFlushVersionsDeduplicatesPerDevice(t *testing.T) {
	conn := &fakeConn{}
	cw, _ := newTestWriter(conn)

	older := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	newer := older.Add(5 * time.Second)
	// Insert newest first to prove dedup picks by UpdatedAt, not order.
	cw.RecordVersion(agentVersionReport{DevicePubkey: "dev1", UpdatedAt: newer, AgentVersion: "v0.9.1"})
	cw.RecordVersion(agentVersionReport{DevicePubkey: "dev1", UpdatedAt: older, AgentVersion: "v0.9.0"})
	cw.RecordVersion(agentVersionReport{DevicePubkey: "dev2", UpdatedAt: older, AgentVersion: "v0.9.0"})
	cw.flush(context.Background())

	rows := conn.batches[0].rows
	if len(rows) != 2 {
		t.Fatalf("versions batch has %d rows, want 2 (one per device)", len(rows))
	}
	for _, row := range rows {
		if row[0] == "dev1" && row[2] != "v0.9.1" {
			t.Errorf("dev1 row kept version %v, want newest v0.9.1", row[2])
		}
	}
}

func TestCreateTablesDegradesPerTable(t *testing.T) {
	conn := &fakeConn{execErrOn: "controller_agent_versions", execErr: errors.New("ACCESS_DENIED")}
	cw, h := newSchemaTestWriter(conn)
	cw.schemaRetryMin, cw.schemaRetryMax = time.Hour, time.Hour

	if err := cw.CreateTables(t.Context()); err != nil {
		t.Fatalf("CreateTables returned %v, want nil when one table is usable", err)
	}
	if events, _, versions := cw.enabled(); !events || versions {
		t.Errorf("enabled flags = (events=%v, versions=%v), want (true, false)", events, versions)
	}
	want := []logEntry{{slog.LevelError, "error creating agent_versions table, disabling version writes"}}
	if got := h.snapshot(); len(got) != 1 || got[0] != want[0] {
		t.Errorf("logs = %+v, want %+v", got, want)
	}
	if writeGauge(writeGetConfigEvents) != 1 || writeGauge(writeCallerIP) != 1 || writeGauge(writeAgentVersions) != 0 {
		t.Errorf("write gauges = (events=%v, caller_ip=%v, versions=%v), want (1, 1, 0)",
			writeGauge(writeGetConfigEvents), writeGauge(writeCallerIP), writeGauge(writeAgentVersions))
	}

	// The disabled stream must not buffer; the enabled one must.
	cw.RecordVersion(agentVersionReport{DevicePubkey: "dev1", AgentVersion: "v0.9.0"})
	cw.Record(getConfigEvent{DevicePubkey: "dev1"})
	if len(cw.versions) != 0 || len(cw.events) != 1 {
		t.Errorf("buffers = (events=%d, versions=%d), want (1, 0)", len(cw.events), len(cw.versions))
	}

	// Both tables failing disables everything and surfaces an error.
	conn = &fakeConn{execErrOn: "CREATE TABLE", execErr: errors.New("ACCESS_DENIED")}
	cw, _ = newSchemaTestWriter(conn)
	if err := cw.CreateTables(t.Context()); err == nil {
		t.Error("CreateTables returned nil, want error when no table is usable")
	}
	if events, callerIP, versions := cw.enabled(); events || callerIP || versions {
		t.Error("expected every write disabled when no table is usable")
	}
	for _, w := range []string{writeGetConfigEvents, writeCallerIP, writeAgentVersions} {
		if writeGauge(w) != 0 {
			t.Errorf("write gauge %s = %v, want 0", w, writeGauge(w))
		}
	}
}

func TestCreateTablesWritesEventsWithoutCallerIPWhenAlterFails(t *testing.T) {
	conn := &fakeConn{execErrOn: "ADD COLUMN IF NOT EXISTS caller_ip", execErr: errors.New("ACCESS_DENIED")}
	cw, h := newSchemaTestWriter(conn)
	cw.schemaRetryMin, cw.schemaRetryMax = time.Hour, time.Hour

	if err := cw.CreateTables(t.Context()); err != nil {
		t.Fatalf("CreateTables returned %v, want nil", err)
	}
	if events, callerIP, versions := cw.enabled(); !events || callerIP || !versions {
		t.Errorf("enabled flags = (events=%v, callerIP=%v, versions=%v), want (true, false, true)", events, callerIP, versions)
	}
	if got := h.snapshot(); len(got) != 1 || got[0] != (logEntry{slog.LevelError, errAddCallerIP}) {
		t.Errorf("logs = %+v, want one ERROR %q", got, errAddCallerIP)
	}
	if writeGauge(writeCallerIP) != 0 {
		t.Errorf("caller_ip write gauge = %v, want 0", writeGauge(writeCallerIP))
	}

	ts := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	cw.Record(getConfigEvent{Timestamp: ts, DevicePubkey: "dev1", CallerIP: "203.0.113.7"})
	cw.flush(context.Background())
	if len(conn.prepared) != 1 || !strings.Contains(conn.prepared[0], "(timestamp, device_pubkey)") {
		t.Fatalf("events INSERT = %v, want the two-column list", conn.prepared)
	}
	if rows := conn.batches[0].rows; len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("events batch rows = %v, want one row of 2 values", rows)
	}

	// A table that could not be created is not altered.
	conn = &fakeConn{execErrOn: "controller_grpc_getconfig_success", execErr: errors.New("ACCESS_DENIED")}
	cw, _ = newSchemaTestWriter(conn)
	cw.schemaRetryMin, cw.schemaRetryMax = time.Hour, time.Hour
	_ = cw.CreateTables(t.Context())
	for _, q := range conn.execsSnapshot() {
		if strings.Contains(q, "ALTER TABLE") {
			t.Errorf("ALTER ran although the getconfig table could not be created: %q", q)
		}
	}
	if _, callerIP, _ := cw.enabled(); callerIP {
		t.Error("expected caller_ip disabled when the getconfig table could not be created")
	}
}

func TestCreateTablesCallerIPColumn(t *testing.T) {
	alters := func(c *fakeConn) int {
		n := 0
		for _, q := range c.execsSnapshot() {
			if strings.Contains(q, "ALTER TABLE") {
				n++
			}
		}
		return n
	}

	// A table without the column gets it added, and events use it.
	conn := &fakeConn{}
	cw, _ := newSchemaTestWriter(conn)
	if err := cw.CreateTables(t.Context()); err != nil {
		t.Fatalf("CreateTables returned %v", err)
	}
	if _, callerIP, _ := cw.enabled(); alters(conn) != 1 || !callerIP {
		t.Errorf("missing column: alters=%d callerIPEnabled=%v, want 1 and true", alters(conn), callerIP)
	}
	if writeGauge(writeCallerIP) != 1 {
		t.Errorf("caller_ip write gauge = %v, want 1", writeGauge(writeCallerIP))
	}
	// The lookup is filtered on this database, table and column.
	wantQuery := `SELECT count() FROM system.columns WHERE database = ? AND table = 'controller_grpc_getconfig_success' AND name = 'caller_ip'`
	if len(conn.queries) != 1 || conn.queries[0] != wantQuery {
		t.Errorf("column lookup = %q, want [%q]", conn.queries, wantQuery)
	}
	if len(conn.queryArgs) != 1 || len(conn.queryArgs[0]) != 1 || conn.queryArgs[0][0] != "testdb" {
		t.Errorf("column lookup args = %v, want [[testdb]]", conn.queryArgs)
	}
	// A table the controller creates has the column from the start.
	for _, q := range conn.execsSnapshot() {
		if strings.Contains(q, "CREATE TABLE IF NOT EXISTS") && strings.Contains(q, "controller_grpc_getconfig_success") && !strings.Contains(q, "caller_ip") {
			t.Errorf("getconfig CREATE has no caller_ip column: %q", q)
		}
	}
	cw.Record(getConfigEvent{DevicePubkey: "dev1", CallerIP: "203.0.113.7"})
	cw.flush(context.Background())
	if len(conn.prepared) != 1 || !strings.Contains(conn.prepared[0], "(timestamp, device_pubkey, caller_ip)") {
		t.Errorf("events INSERT = %v, want the three-column list", conn.prepared)
	}

	// A table that has the column is not altered, so no ALTER grant is needed.
	conn = &fakeConn{hasCallerIP: true, execErrOn: "ALTER TABLE", execErr: errors.New("ACCESS_DENIED")}
	cw, h := newSchemaTestWriter(conn)
	if err := cw.CreateTables(t.Context()); err != nil {
		t.Fatalf("CreateTables returned %v", err)
	}
	if _, callerIP, _ := cw.enabled(); alters(conn) != 0 || !callerIP || len(h.snapshot()) != 0 {
		t.Errorf("existing column: alters=%d callerIPEnabled=%v logs=%+v, want 0, true, none", alters(conn), callerIP, h.snapshot())
	}

	// A failed lookup leaves the column off and logs it.
	conn = &fakeConn{queryErr: errors.New("ACCESS_DENIED")}
	cw, h = newSchemaTestWriter(conn)
	cw.schemaRetryMin, cw.schemaRetryMax = time.Hour, time.Hour
	if err := cw.CreateTables(t.Context()); err != nil {
		t.Fatalf("CreateTables returned %v", err)
	}
	got := h.snapshot()
	if _, callerIP, _ := cw.enabled(); alters(conn) != 0 || callerIP || len(got) != 1 || got[0] != (logEntry{slog.LevelError, errAddCallerIP}) {
		t.Errorf("failed lookup: alters=%d callerIPEnabled=%v logs=%+v, want 0, false, one ERROR %q", alters(conn), callerIP, got, errAddCallerIP)
	}
}

// eventually polls cond until it holds or a second passes.
func eventually(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

func TestCreateTablesRetriesFailedStepsUntilTheySucceed(t *testing.T) {
	// The lookup fails and the versions table cannot be created at startup.
	conn := &fakeConn{queryErr: errors.New("connection reset"), execErrOn: "controller_agent_versions", execErr: errors.New("ACCESS_DENIED")}
	cw, h := newSchemaTestWriter(conn)

	if err := cw.CreateTables(t.Context()); err != nil {
		t.Fatalf("CreateTables returned %v", err)
	}
	if _, callerIP, versions := cw.enabled(); callerIP || versions {
		t.Fatalf("after startup: callerIP=%v versions=%v, want both false", callerIP, versions)
	}

	// Retries keep failing at WARN without enabling anything.
	if !eventually(t, func() bool { return len(h.snapshot()) >= 4 }) {
		t.Fatalf("no retries logged: %+v", h.snapshot())
	}
	for _, e := range h.snapshot()[2:] {
		if e.Level != slog.LevelWarn {
			t.Errorf("retry logged %+v, want WARN", e)
		}
	}

	// Once ClickHouse recovers, the retry enables both and stops.
	conn.set(func(c *fakeConn) { c.queryErr, c.execErrOn = nil, "" })
	if !eventually(t, func() bool { _, c, v := cw.enabled(); return c && v }) {
		t.Fatal("caller_ip and versions not enabled after the failures cleared")
	}
	if writeGauge(writeCallerIP) != 1 || writeGauge(writeAgentVersions) != 1 {
		t.Errorf("write gauges = (caller_ip=%v, versions=%v), want (1, 1)", writeGauge(writeCallerIP), writeGauge(writeAgentVersions))
	}
	if !eventually(t, func() bool {
		got := h.snapshot()
		return got[len(got)-1].Level == slog.LevelInfo
	}) {
		t.Fatal("no INFO line when the retry succeeded")
	}
	n := len(conn.execsSnapshot())
	time.Sleep(20 * time.Millisecond)
	if got := len(conn.execsSnapshot()); got != n {
		t.Errorf("retry loop kept running after success: %d execs, then %d", n, got)
	}
}

func TestCreateTablesRetryStopsWithContext(t *testing.T) {
	conn := &fakeConn{queryErr: errors.New("connection reset")}
	cw, _ := newSchemaTestWriter(conn)
	ctx, cancel := context.WithCancel(t.Context())
	if err := cw.CreateTables(ctx); err != nil {
		t.Fatalf("CreateTables returned %v", err)
	}
	cancel()
	time.Sleep(10 * time.Millisecond)
	conn.mu.Lock()
	n := len(conn.queries)
	conn.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.queries) != n {
		t.Errorf("retry loop kept running after ctx ended: %d lookups, then %d", n, len(conn.queries))
	}
}

func TestCreateTablesRunsOnceForBothListeners(t *testing.T) {
	conn := &fakeConn{queryErr: errors.New("connection reset")}
	cw, h := newSchemaTestWriter(conn)
	cw.schemaRetryMin, cw.schemaRetryMax = time.Hour, time.Hour

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = cw.CreateTables(t.Context())
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("listener %d: CreateTables returned %v", i, err)
		}
	}
	creates := 0
	for _, q := range conn.execsSnapshot() {
		if strings.Contains(q, "CREATE TABLE") {
			creates++
		}
	}
	conn.mu.Lock()
	lookups := len(conn.queries)
	conn.mu.Unlock()
	if creates != 2 || lookups != 1 || len(h.snapshot()) != 1 {
		t.Errorf("two listeners ran %d CREATEs, %d lookups and logged %d lines, want 2, 1 and 1", creates, lookups, len(h.snapshot()))
	}
}

func TestCallerIP(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"ipv4", peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51234}}), "203.0.113.7"},
		{"ipv6", peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 51234}}), "2001:db8::1"},
		{"no peer", context.Background(), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := callerIP(tt.ctx); got != tt.want {
				t.Errorf("callerIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRecordVersion(t *testing.T) {
	cw, _ := newTestWriter(&fakeConn{})

	// A blank agent report must not be recorded: ReplacingMergeTree keeps the
	// row with the newest updated_at, so it would overwrite the last known
	// good version row.
	cw.RecordVersion(agentVersionReport{DevicePubkey: "dev1"})
	if len(cw.versions) != 0 {
		t.Fatalf("blank agent report was recorded, want skipped")
	}

	long := strings.Repeat("x", maxAgentFieldLen+50)
	cw.RecordVersion(agentVersionReport{DevicePubkey: "dev1", AgentVersion: "v0.9.0", AgentCommit: long})
	if len(cw.versions) != 1 {
		t.Fatalf("expected 1 recorded version event, got %d", len(cw.versions))
	}
	got := cw.versions[0]
	if got.ControllerVersion != "v1.2.3" || got.ControllerCommit != "abc123" || got.ControllerDate != "2026-01-01" {
		t.Errorf("controller build info not stamped: %+v", got)
	}
	if got.AgentVersion != "v0.9.0" {
		t.Errorf("AgentVersion = %q, want %q", got.AgentVersion, "v0.9.0")
	}
	if len(got.AgentCommit) != maxAgentFieldLen {
		t.Errorf("AgentCommit length = %d, want truncated to %d", len(got.AgentCommit), maxAgentFieldLen)
	}
}

func TestBuildClickhouseOptions(t *testing.T) {
	tests := []struct {
		name       string
		addr       string
		db         string
		user       string
		pass       string
		disableTLS bool
		wantAddr   string
		wantTLS    bool
	}{
		{
			name:       "production defaults (HTTPS)",
			addr:       "clickhouse.example.com:8443",
			db:         "mydb",
			user:       "admin",
			pass:       "secret",
			disableTLS: false,
			wantAddr:   "clickhouse.example.com:8443",
			wantTLS:    true,
		},
		{
			name:       "TLS disabled (plain HTTP for local dev)",
			addr:       "localhost:8123",
			db:         "default",
			user:       "default",
			pass:       "",
			disableTLS: true,
			wantAddr:   "localhost:8123",
			wantTLS:    false,
		},
		{
			name:       "strips https:// scheme prefix",
			addr:       "https://clickhouse.example.com:8443",
			db:         "default",
			user:       "default",
			pass:       "",
			disableTLS: false,
			wantAddr:   "clickhouse.example.com:8443",
			wantTLS:    true,
		},
		{
			name:       "strips http:// scheme prefix",
			addr:       "http://localhost:8123",
			db:         "default",
			user:       "default",
			pass:       "",
			disableTLS: true,
			wantAddr:   "localhost:8123",
			wantTLS:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := buildClickhouseOptions(tt.addr, tt.db, tt.user, tt.pass, tt.disableTLS)

			// Core fix: protocol must always be HTTP (not native TCP)
			if opts.Protocol != clickhouse.HTTP {
				t.Errorf("Protocol = %v, want clickhouse.HTTP (%v)", opts.Protocol, clickhouse.HTTP)
			}

			if opts.ConnMaxLifetime != 3*time.Minute {
				t.Errorf("ConnMaxLifetime = %v, want 3m", opts.ConnMaxLifetime)
			}

			// Verify address scheme stripping
			if len(opts.Addr) != 1 || opts.Addr[0] != tt.wantAddr {
				t.Errorf("Addr = %v, want [%s]", opts.Addr, tt.wantAddr)
			}

			// Verify TLS configuration
			if tt.wantTLS && opts.TLS == nil {
				t.Error("TLS = nil, want non-nil (HTTPS mode)")
			}
			if !tt.wantTLS && opts.TLS != nil {
				t.Error("TLS = non-nil, want nil (plain HTTP mode)")
			}

			// Verify auth
			if opts.Auth.Database != tt.db {
				t.Errorf("Auth.Database = %q, want %q", opts.Auth.Database, tt.db)
			}
			if opts.Auth.Username != tt.user {
				t.Errorf("Auth.Username = %q, want %q", opts.Auth.Username, tt.user)
			}
			if opts.Auth.Password != tt.pass {
				t.Errorf("Auth.Password = %q, want %q", opts.Auth.Password, tt.pass)
			}
		})
	}
}
