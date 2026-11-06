package allowlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLedger struct {
	pd  *serviceability.ProgramData
	err error
}

func (f *fakeLedger) GetProgramData(context.Context) (*serviceability.ProgramData, error) {
	return f.pd, f.err
}

type fakeCallers struct {
	calls []Call
	err   error
	since time.Time
}

func (f *fakeCallers) RecentCalls(_ context.Context, since time.Time) ([]Call, error) {
	f.since = since
	return f.calls, f.err
}

type fakeConfigMap struct {
	cm        *ConfigMap // nil: not found
	getErr    error
	updateErr error
	updates   int
}

func (f *fakeConfigMap) Get(context.Context) (*ConfigMap, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.cm == nil {
		return nil, ErrConfigMapNotFound
	}
	c := *f.cm
	return &c, nil
}

func (f *fakeConfigMap) Update(_ context.Context, cm *ConfigMap) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updates++
	f.cm = cm
	return nil
}

func (f *fakeConfigMap) gnmicMap(t *testing.T) GnmicAddressMap {
	t.Helper()
	var m GnmicAddressMap
	require.NoError(t, json.Unmarshal([]byte(f.cm.Data["map.json"]), &m))
	return m
}

type fakeIPList struct {
	items      []ListItem
	itemsErr   error
	replaceErr error
	replaces   int
}

func (f *fakeIPList) Items(context.Context) ([]ListItem, error) {
	return append([]ListItem(nil), f.items...), f.itemsErr
}

func (f *fakeIPList) ReplaceItems(_ context.Context, items []ListItem) error {
	if f.replaceErr != nil {
		return f.replaceErr
	}
	f.replaces++
	f.items = items
	return nil
}

type harness struct {
	ledger  *fakeLedger
	callers *fakeCallers
	cm      *fakeConfigMap
	cf      *fakeIPList
	metrics *Metrics
	clock   *clockwork.FakeClock
	excPath string
	cfg     Config
}

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// fleet returns n Activated devices, each with one address.
func fleet(n int) []serviceability.Device {
	out := make([]serviceability.Device, n)
	for i := range out {
		out[i] = dev(byte(i+1), fmt.Sprintf("dz%d", i+1), serviceability.DeviceStatusActivated, fmt.Sprintf("45.0.0.%d", i+1))
	}
	return out
}

func newHarness(t *testing.T, devices []serviceability.Device) *harness {
	t.Helper()
	h := &harness{
		ledger:  &fakeLedger{pd: &serviceability.ProgramData{GlobalState: &serviceability.GlobalState{}, Devices: devices}},
		callers: &fakeCallers{},
		cm:      &fakeConfigMap{cm: &ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Metadata: json.RawMessage(`{"name":"gnmic-device-addresses","resourceVersion":"7"}`)}},
		cf:      &fakeIPList{},
		metrics: NewMetrics(prometheus.NewRegistry()),
		clock:   clockwork.NewFakeClockAt(t0),
		excPath: filepath.Join(t.TempDir(), "exceptions.yaml"),
	}
	h.writeExceptions(t, "")
	h.cfg = Config{
		Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:          h.clock,
		Metrics:        h.metrics,
		Ledger:         h.ledger,
		ExceptionsFile: h.excPath,
		Callers:        h.callers,
		CallerWindow:   24 * time.Hour,
		Interval:       time.Minute,
		PassTimeout:    time.Minute,
	}
	return h
}

func (h *harness) writeExceptions(t *testing.T, raw string) {
	t.Helper()
	require.NoError(t, os.WriteFile(h.excPath, []byte(raw), 0o600))
}

// pass builds a Syncer from h.cfg, as it stands, and runs one pass.
func (h *harness) pass(t *testing.T) error {
	t.Helper()
	cfg := h.cfg
	cfg.Targets = []Target{&GnmicMapTarget{API: h.cm, Key: "map.json"}, &CloudflareTarget{API: h.cf}}
	s, err := New(cfg)
	require.NoError(t, err)
	return s.Pass(context.Background())
}

func (h *harness) cfIPs() []string {
	var out []string
	for _, it := range h.cf.items {
		out = append(out, it.IP)
	}
	sort.Strings(out)
	return out
}

func (h *harness) wrote() (int, int) { return h.cm.updates, h.cf.replaces }

func counter(c *prometheus.CounterVec, label string) float64 {
	return testutil.ToFloat64(c.WithLabelValues(label))
}

func TestPass_WritesBothTargets(t *testing.T) {
	devices := []serviceability.Device{
		dev(1, "dz1", serviceability.DeviceStatusActivated, "45.1.1.1", "45.1.1.9/31"),
		dev(2, "dz2", serviceability.DeviceStatusDrained, "45.2.2.2"),
	}
	h := newHarness(t, devices)
	h.cm.cm.Data = map[string]string{"other": "kept"}
	require.NoError(t, h.pass(t))

	assert.Equal(t, GnmicAddressMap{pk(1): {"45.1.1.1", "45.1.1.9"}, pk(2): {"45.2.2.2"}}, h.cm.gnmicMap(t))
	assert.Equal(t, "kept", h.cm.cm.Data["other"], "other keys survive")
	assert.JSONEq(t, `{"name":"gnmic-device-addresses","resourceVersion":"7"}`, string(h.cm.cm.Metadata), "metadata, and so resourceVersion, is sent back")
	assert.Equal(t, []ListItem{{IP: "45.1.1.1", Comment: "dz1"}, {IP: "45.1.1.9", Comment: "dz1"}, {IP: "45.2.2.2", Comment: "dz2"}}, h.cf.items)
	assert.Equal(t, float64(t0.Unix()), testutil.ToFloat64(h.metrics.LastSuccess))
	assert.Equal(t, 1.0, counter(h.metrics.Writes, "gnmic_map"))
	assert.Equal(t, 1.0, counter(h.metrics.Writes, "cloudflare_list"))
	assert.Equal(t, 3.0, testutil.ToFloat64(h.metrics.ListedAddresses))
	assert.Equal(t, 2.0, testutil.ToFloat64(h.metrics.ListedDevices))
}

func TestPass_WritesOnlyWhenSomethingChanged(t *testing.T) {
	h := newHarness(t, fleet(3))
	require.NoError(t, h.pass(t))
	cmUpdates, cfReplaces := h.wrote()
	require.Equal(t, [2]int{1, 1}, [2]int{cmUpdates, cfReplaces})

	h.clock.Advance(time.Minute)
	require.NoError(t, h.pass(t))
	cmUpdates, cfReplaces = h.wrote()
	assert.Equal(t, [2]int{1, 1}, [2]int{cmUpdates, cfReplaces}, "an unchanged list writes nothing")
	assert.Equal(t, float64(t0.Add(time.Minute).Unix()), testutil.ToFloat64(h.metrics.LastSuccess), "an unchanged pass still succeeds")

	// A new device reaches both.
	h.ledger.pd.Devices = append(h.ledger.pd.Devices, dev(9, "dz9", serviceability.DeviceStatusActivated, "45.0.0.9"))
	require.NoError(t, h.pass(t))
	cmUpdates, cfReplaces = h.wrote()
	assert.Equal(t, [2]int{2, 2}, [2]int{cmUpdates, cfReplaces})
	assert.Equal(t, []string{"45.0.0.1", "45.0.0.2", "45.0.0.3", "45.0.0.9"}, h.cfIPs())

	// Only a Cloudflare comment changes: only Cloudflare is written.
	h.ledger.pd.Devices[3].Code = "dz9-renamed"
	require.NoError(t, h.pass(t))
	cmUpdates, cfReplaces = h.wrote()
	assert.Equal(t, [2]int{2, 3}, [2]int{cmUpdates, cfReplaces})
}

func TestPass_ReadFailuresWriteNothing(t *testing.T) {
	malformed := dev(9, "dz9", serviceability.DeviceStatusActivated, "45.0.0.9")
	malformed.DeserializeError = errors.New("trailing interfaces length mismatch")

	cases := map[string]struct {
		breakIt func(h *harness)
		stage   string
		err     string
	}{
		"ledger error": {func(h *harness) { h.ledger.err = errors.New("rpc down") }, "ledger", "ledger: rpc down"},
		"no devices": {func(h *harness) { h.ledger.pd.Devices = nil }, "ledger",
			"ledger: incomplete ledger read: no device accounts"},
		"no global state": {func(h *harness) { h.ledger.pd.GlobalState = nil }, "ledger",
			"ledger: incomplete ledger read: no global state account"},
		"malformed device": {func(h *harness) { h.ledger.pd.Devices = append(h.ledger.pd.Devices, malformed) }, "ledger",
			"ledger: incomplete ledger read: device " + pk(9) + " (dz9) did not deserialize: trailing interfaces length mismatch"},
		"exception file missing": {func(h *harness) { h.cfg.ExceptionsFile = "/nonexistent/exceptions.yaml" }, "exceptions",
			"exceptions: reading exception file: open /nonexistent/exceptions.yaml: no such file or directory"},
		"configmap read":   {func(h *harness) { h.cm.getErr = errors.New("forbidden") }, "read_gnmic_map", "read_gnmic_map: forbidden"},
		"configmap absent": {func(h *harness) { h.cm.cm = nil }, "read_gnmic_map", "read_gnmic_map: configmap not found"},
		"configmap garbage": {func(h *harness) { h.cm.cm.Data = map[string]string{"map.json": "{"} }, "read_gnmic_map",
			`read_gnmic_map: configmap key "map.json" holds no address map, so the sync cannot tell what a write would remove; fix or empty the key: unexpected end of JSON input`},
		// The map read succeeds, and still nothing is written to it.
		"cloudflare read": {func(h *harness) { h.cf.itemsErr = errors.New("HTTP 403") }, "read_cloudflare_list", "read_cloudflare_list: HTTP 403"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, fleet(3))
			tc.breakIt(h)
			err := h.pass(t)
			assert.EqualError(t, err, tc.err)
			cmUpdates, cfReplaces := h.wrote()
			assert.Equal(t, [2]int{0, 0}, [2]int{cmUpdates, cfReplaces})
			assert.Equal(t, 1.0, counter(h.metrics.Errors, tc.stage))
			assert.Equal(t, 0.0, testutil.ToFloat64(h.metrics.LastSuccess))
		})
	}
}

func TestPass_InvalidExceptionFileWritesNothing(t *testing.T) {
	h := newHarness(t, fleet(3))
	require.NoError(t, h.pass(t))
	h.writeExceptions(t, "exclusions:\n  - pubkey: "+pk(1)+"\n    code: dz1\n    reason: \"\"\n")
	h.ledger.pd.Devices = append(h.ledger.pd.Devices, dev(9, "dz9", serviceability.DeviceStatusActivated, "45.0.0.9"))
	assert.EqualError(t, h.pass(t), "exceptions: exception file: exclusion 0 (dz1): reason is empty")
	cmUpdates, cfReplaces := h.wrote()
	assert.Equal(t, [2]int{1, 1}, [2]int{cmUpdates, cfReplaces})
	assert.Equal(t, []string{"45.0.0.1", "45.0.0.2", "45.0.0.3"}, h.cfIPs())
}

func TestPass_NeverWritesAnEmptyList(t *testing.T) {
	h := newHarness(t, []serviceability.Device{dev(1, "dz1", serviceability.DeviceStatusDeleting, "45.0.0.1")})
	err := h.pass(t)
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, RefusedEmpty, refused.Reason)
	assert.Equal(t, 1.0, counter(h.metrics.RefusedPasses, RefusedEmpty))
	cmUpdates, cfReplaces := h.wrote()
	assert.Equal(t, [2]int{0, 0}, [2]int{cmUpdates, cfReplaces})

	// A live device whose every address is private builds an empty list too.
	h.ledger.pd.Devices = []serviceability.Device{dev(1, "dz1", serviceability.DeviceStatusActivated, "10.0.0.1")}
	require.ErrorAs(t, h.pass(t), &refused)
	assert.Equal(t, 2.0, counter(h.metrics.RefusedPasses, RefusedEmpty))
}

func TestPass_RemovalLimits(t *testing.T) {
	cases := map[string]struct {
		fleet, keep int
		refused     string
	}{
		// 60 devices of one address each: 5 removed is 8%, 6 is 10%.
		"5 devices removed is allowed": {60, 55, ""},
		"6 devices removed is refused": {60, 54, RefusedTooManyDevices},
		// 20 devices: 2 removed is 10% exactly, 3 is 15%.
		"10% of addresses removed is allowed":      {20, 18, ""},
		"over 10% of addresses removed is refused": {20, 17, RefusedTooManyAddrs},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, fleet(tc.fleet))
			require.NoError(t, h.pass(t))
			h.ledger.pd.Devices = h.ledger.pd.Devices[:tc.keep]
			err := h.pass(t)
			cmUpdates, cfReplaces := h.wrote()
			if tc.refused == "" {
				require.NoError(t, err)
				assert.Equal(t, [2]int{2, 2}, [2]int{cmUpdates, cfReplaces})
				assert.Len(t, h.cfIPs(), tc.keep)
				return
			}
			var refused *RefusedError
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, tc.refused, refused.Reason)
			assert.Contains(t, refused.Detail, "set --allow-shrink-to="+mustBuild(t, h.ledger.pd.Devices, Exceptions{}).List.Digest())
			assert.Equal(t, 1.0, counter(h.metrics.RefusedPasses, tc.refused))
			assert.Equal(t, [2]int{1, 1}, [2]int{cmUpdates, cfReplaces}, "a refused pass writes nothing")
			assert.Len(t, h.cfIPs(), tc.fleet)
			assert.Len(t, h.cm.gnmicMap(t), tc.fleet)
		})
	}
}

// The Cloudflare list is checked against what it holds, not only the map: a
// list out of step with the map is not shrunk past the limit either.
func TestPass_RemovalLimitAppliesToCloudflareOnItsOwn(t *testing.T) {
	h := newHarness(t, fleet(10))
	require.NoError(t, h.pass(t))
	for i := range 3 {
		h.cf.items = append(h.cf.items, ListItem{IP: fmt.Sprintf("45.9.9.%d", i+1)})
	}
	h.cf.items = append(h.cf.items, ListItem{IP: "45.8.0.0/16"})
	err := h.pass(t)
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, RefusedTooManyAddrs, refused.Reason)
	assert.Contains(t, refused.Detail, "cloudflare_list would remove 4 of 14 addresses")
	_, cfReplaces := h.wrote()
	assert.Equal(t, 1, cfReplaces)
}

func TestPass_AllowShrinkToAppliesOnlyThatList(t *testing.T) {
	h := newHarness(t, fleet(20))
	require.NoError(t, h.pass(t))
	h.ledger.pd.Devices = h.ledger.pd.Devices[:10]

	var refused *RefusedError
	require.ErrorAs(t, h.pass(t), &refused)
	digest := mustBuild(t, h.ledger.pd.Devices, Exceptions{}).List.Digest()
	assert.Contains(t, refused.Detail, "set --allow-shrink-to="+digest+" to apply this list once")

	// A digest for another list does not unlock this one.
	h.cfg.AllowShrinkTo = mustBuild(t, h.ledger.pd.Devices[:9], Exceptions{}).List.Digest()
	require.ErrorAs(t, h.pass(t), &refused)
	cmUpdates, cfReplaces := h.wrote()
	assert.Equal(t, [2]int{1, 1}, [2]int{cmUpdates, cfReplaces})

	h.cfg.AllowShrinkTo = digest
	require.NoError(t, h.pass(t))
	assert.Len(t, h.cfIPs(), 10)
	assert.Len(t, h.cm.gnmicMap(t), 10)

	// Left set, it does nothing to a later shrink.
	h.ledger.pd.Devices = h.ledger.pd.Devices[:5]
	require.ErrorAs(t, h.pass(t), &refused)
	assert.Len(t, h.cfIPs(), 10)
}

func TestPass_DryRunWritesNothing(t *testing.T) {
	h := newHarness(t, fleet(3))
	h.cfg.DryRun = true
	require.NoError(t, h.pass(t))
	cmUpdates, cfReplaces := h.wrote()
	assert.Equal(t, [2]int{0, 0}, [2]int{cmUpdates, cfReplaces})
	assert.Equal(t, float64(t0.Unix()), testutil.ToFloat64(h.metrics.LastSuccess))
	assert.Equal(t, 1.0, testutil.ToFloat64(h.metrics.DryRun), "a dry run shows, since it still stamps last_success")

	// Dry run still refuses what a real pass would refuse.
	h.cfg.DryRun = false
	require.NoError(t, h.pass(t))
	assert.Equal(t, 0.0, testutil.ToFloat64(h.metrics.DryRun))
	h.cfg.DryRun = true
	h.ledger.pd.Devices = h.ledger.pd.Devices[:1]
	var refused *RefusedError
	require.ErrorAs(t, h.pass(t), &refused)
	assert.Equal(t, 1.0, counter(h.metrics.RefusedPasses, RefusedTooManyAddrs))
}

func TestPass_FailedWriteIsRetriedNextPass(t *testing.T) {
	h := newHarness(t, fleet(3))
	h.cm.updateErr = errors.New("409 conflict")
	assert.EqualError(t, h.pass(t), "write gnmic_map: 409 conflict")
	cmUpdates, cfReplaces := h.wrote()
	assert.Equal(t, [2]int{0, 1}, [2]int{cmUpdates, cfReplaces}, "a failed map write does not hold back Cloudflare")
	assert.Equal(t, 1.0, counter(h.metrics.Errors, "write_gnmic_map"))
	assert.Equal(t, 0.0, testutil.ToFloat64(h.metrics.LastSuccess))

	h.cm.updateErr = nil
	require.NoError(t, h.pass(t))
	cmUpdates, cfReplaces = h.wrote()
	assert.Equal(t, [2]int{1, 1}, [2]int{cmUpdates, cfReplaces}, "only the target that failed is written again")
	assert.Len(t, h.cm.gnmicMap(t), 3)
}

func TestPass_ReportFailureDoesNotBlockTheList(t *testing.T) {
	h := newHarness(t, fleet(2))
	h.callers.calls = []Call{{DevicePubkey: pk(1), CallerIP: netip.MustParseAddr("8.8.8.8"), LastSeen: t0}}
	require.NoError(t, h.pass(t))
	assert.Equal(t, t0.Add(-24*time.Hour), h.callers.since)
	offchain := `
# HELP doublezero_telemetry_allowlist_sync_offchain_caller_last_seen_timestamp_seconds Newest call per device and calling address the ledger does not hold for that device.
# TYPE doublezero_telemetry_allowlist_sync_offchain_caller_last_seen_timestamp_seconds gauge
doublezero_telemetry_allowlist_sync_offchain_caller_last_seen_timestamp_seconds{caller_ip="8.8.8.8",code="dz1",listed="false",pubkey="` + pk(1) + `"} 1.7939664e+09
`
	require.NoError(t, testutil.CollectAndCompare(h.metrics.OffchainCaller, strings.NewReader(offchain)))

	h.clock.Advance(time.Minute)
	h.callers.err = errors.New("clickhouse down")
	h.ledger.pd.Devices = append(h.ledger.pd.Devices, dev(9, "dz9", serviceability.DeviceStatusActivated, "45.0.0.9"))
	require.NoError(t, h.pass(t))
	assert.Equal(t, []string{"45.0.0.1", "45.0.0.2", "45.0.0.9"}, h.cfIPs())
	assert.Equal(t, 1.0, counter(h.metrics.Errors, "report"))
	assert.Equal(t, float64(t0.Unix()), testutil.ToFloat64(h.metrics.ReportLastSuccess), "the report's age shows")
	assert.Equal(t, float64(t0.Add(time.Minute).Unix()), testutil.ToFloat64(h.metrics.LastSuccess))
	require.NoError(t, testutil.CollectAndCompare(h.metrics.OffchainCaller, strings.NewReader(offchain)), "the last report stays")
}

func TestPass_DeviceGauges(t *testing.T) {
	h := newHarness(t, []serviceability.Device{
		dev(1, "dz1", serviceability.DeviceStatusActivated, "45.0.0.1"),
		dev(2, "dz2", serviceability.DeviceStatusLinkProvisioning, "45.0.0.2"),
		dev(3, "excluded", serviceability.DeviceStatusActivated, "45.0.0.3"),
		dev(4, "deleting", serviceability.DeviceStatusDeleting, "45.0.0.4"),
	})
	h.writeExceptions(t, "exclusions:\n  - pubkey: "+pk(3)+"\n    code: excluded\n    reason: x\n")
	h.callers.calls = []Call{{DevicePubkey: pk(1), CallerIP: netip.MustParseAddr("45.0.0.1"), LastSeen: t0}}
	require.NoError(t, h.pass(t))

	live := `
# HELP doublezero_telemetry_allowlist_sync_live_device 1 for each live device that is not excluded.
# TYPE doublezero_telemetry_allowlist_sync_live_device gauge
doublezero_telemetry_allowlist_sync_live_device{code="dz1",pubkey="` + pk(1) + `"} 1
doublezero_telemetry_allowlist_sync_live_device{code="dz2",pubkey="` + pk(2) + `"} 1
`
	require.NoError(t, testutil.CollectAndCompare(h.metrics.LiveDevice, strings.NewReader(live)))
	lastCall := `
# HELP doublezero_telemetry_allowlist_sync_device_last_listed_call_timestamp_seconds Newest call from a device's own listed addresses, 0 when none in the caller window.
# TYPE doublezero_telemetry_allowlist_sync_device_last_listed_call_timestamp_seconds gauge
doublezero_telemetry_allowlist_sync_device_last_listed_call_timestamp_seconds{code="dz1",pubkey="` + pk(1) + `"} 1.7939664e+09
doublezero_telemetry_allowlist_sync_device_last_listed_call_timestamp_seconds{code="dz2",pubkey="` + pk(2) + `"} 0
`
	require.NoError(t, testutil.CollectAndCompare(h.metrics.LastListedCall, strings.NewReader(lastCall)))

	// A device that stops being live loses its series, even when the removal
	// limit refuses to write the shorter list: the gauges follow the ledger.
	h.ledger.pd.Devices[1].Status = serviceability.DeviceStatusDeleting
	var refused *RefusedError
	require.ErrorAs(t, h.pass(t), &refused)
	assert.Equal(t, 1, testutil.CollectAndCount(h.metrics.LiveDevice))
	assert.Equal(t, 1, testutil.CollectAndCount(h.metrics.LastListedCall))
}

func TestRun_PassesAtStartAndEveryInterval(t *testing.T) {
	h := newHarness(t, fleet(3))
	cfg := h.cfg
	cfg.Targets = []Target{&GnmicMapTarget{API: h.cm, Key: "map.json"}, &CloudflareTarget{API: h.cf}}
	s, err := New(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool { return testutil.ToFloat64(h.metrics.LastSuccess) == float64(t0.Unix()) }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, h.clock.BlockUntilContext(ctx, 1))
	h.clock.Advance(time.Minute)
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(h.metrics.LastSuccess) == float64(t0.Add(time.Minute).Unix())
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}

func TestNew_RequiresConfig(t *testing.T) {
	h := newHarness(t, fleet(1))
	cfg := h.cfg
	_, err := New(cfg)
	assert.EqualError(t, err, "at least one target is required")
	cfg.Targets = []Target{&CloudflareTarget{API: h.cf}}
	cfg.ExceptionsFile = ""
	_, err = New(cfg)
	assert.EqualError(t, err, "exceptions file is required")
}

// Cloudflare may return a /32 for an address the sync wrote bare: that is
// not a change.
func TestPass_CloudflareSlash32IsTheSameItem(t *testing.T) {
	h := newHarness(t, fleet(2))
	require.NoError(t, h.pass(t))
	for i := range h.cf.items {
		h.cf.items[i].IP += "/32"
	}
	require.NoError(t, h.pass(t))
	_, cfReplaces := h.wrote()
	assert.Equal(t, 1, cfReplaces)
}

func TestGnmicMapTarget_ApplyNeedsAFreshPlan(t *testing.T) {
	h := newHarness(t, fleet(1))
	target := &GnmicMapTarget{API: h.cm, Key: "map.json"}
	assert.EqualError(t, target.Apply(context.Background(), Plan{}), "apply without a plan")
	list := mustBuild(t, h.ledger.pd.Devices, Exceptions{}).List
	plan, err := target.Plan(context.Background(), list)
	require.NoError(t, err)
	require.NoError(t, target.Apply(context.Background(), plan))
	assert.EqualError(t, target.Apply(context.Background(), plan), "apply without a plan", "a second apply would reuse the old resourceVersion")
}
