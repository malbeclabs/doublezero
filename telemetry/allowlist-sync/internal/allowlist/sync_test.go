package allowlist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	cut   bool
	err   error
	since time.Time
}

func (f *fakeCallers) RecentCalls(_ context.Context, since time.Time) ([]Call, bool, error) {
	f.since = since
	return f.calls, f.cut, f.err
}

type fakeConfigMap struct {
	cm        *ConfigMap // nil: not found
	getErr    error
	updateErr error
	// commitThenErr applies the update and then fails, as a timeout after the
	// API server committed it does.
	commitThenErr bool
	updates       int
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
	if f.updateErr != nil && !f.commitThenErr {
		return f.updateErr
	}
	f.updates++
	f.cm = cm
	return f.updateErr
}

func (f *fakeConfigMap) gnmicMap(t *testing.T) GnmicAddressMap {
	t.Helper()
	var m GnmicAddressMap
	require.NoError(t, json.Unmarshal([]byte(f.cm.Data["map.json"]), &m))
	return m
}

// fakeIPList holds items with an id and a creation time, and deletes them by
// id. Cloudflare's docs do not say what adding an address the list already
// holds does, so the fake does what appendExisting says, never an update:
// refuse the whole add (the default) or add a second item. ops records each
// write in order.
type fakeIPList struct {
	items          []ListItem
	itemsErr       error
	addErr         error
	deleteErr      error
	appendExisting bool
	nextID         int
	ops            []string
}

func (f *fakeIPList) newID() (string, string) {
	f.nextID++
	return fmt.Sprintf("item%d", f.nextID), fmt.Sprintf("2026-01-01T00:%02d:%02dZ", f.nextID/60, f.nextID%60)
}

func (f *fakeIPList) Items(context.Context) ([]ListItem, error) {
	for i := range f.items {
		if f.items[i].ID == "" {
			f.items[i].ID, f.items[i].CreatedOn = f.newID()
		}
	}
	return append([]ListItem(nil), f.items...), f.itemsErr
}

func (f *fakeIPList) AddItems(_ context.Context, items []ListItem) error {
	if f.addErr != nil {
		return f.addErr
	}
	ips := make([]string, 0, len(items))
	for _, it := range items {
		if it.ID != "" {
			return fmt.Errorf("added item %s carries id %s", it.IP, it.ID)
		}
		held := slices.ContainsFunc(f.items, func(c ListItem) bool { k, _ := listKey(c.IP); return k == it.IP })
		if held && !f.appendExisting {
			return fmt.Errorf("bulk operation failed: %s is already in the list", it.IP)
		}
		ips = append(ips, it.IP)
	}
	for _, it := range items {
		it.ID, it.CreatedOn = f.newID()
		f.items = append(f.items, it)
	}
	f.ops = append(f.ops, "add "+strings.Join(ips, ","))
	return nil
}

func (f *fakeIPList) DeleteItems(_ context.Context, ids []string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.items = slices.DeleteFunc(f.items, func(c ListItem) bool { return slices.Contains(ids, c.ID) })
	f.ops = append(f.ops, "delete "+strings.Join(ids, ","))
	return nil
}

// cfItems is the list as IP and comment, without the ids.
func (h *harness) cfItems() []ListItem {
	out := make([]ListItem, 0, len(h.cf.items))
	for _, it := range h.cf.items {
		out = append(out, ListItem{IP: it.IP, Comment: it.Comment})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out
}

type harness struct {
	ledger  *fakeLedger
	callers *fakeCallers
	cm      *fakeConfigMap
	cf      *fakeIPList
	// cmTarget and cfTarget are kept across passes, as the sync's own are.
	cmTarget *GnmicMapTarget
	cfTarget *CloudflareTarget
	metrics  *Metrics
	clock    *clockwork.FakeClock
	excPath  string
	cfg      Config
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
	h.restart()
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
	cfg.Targets = []Target{h.cmTarget, h.cfTarget}
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

// restart gives the harness new targets, which remember nothing, as after a
// process start.
func (h *harness) restart() {
	h.cmTarget = &GnmicMapTarget{API: h.cm, Key: "map.json"}
	h.cfTarget = &CloudflareTarget{API: h.cf}
}

// external reads external_changes_total for one target.
func (h *harness) external(target string) float64 {
	return testutil.ToFloat64(h.metrics.ExternalChanges.WithLabelValues(target))
}

// wrote counts the writes to the map, and the passes that wrote Cloudflare.
func (h *harness) wrote() (int, int) {
	return h.cm.updates, int(testutil.ToFloat64(h.metrics.Writes.WithLabelValues("cloudflare_list")))
}

func counter(c *prometheus.CounterVec, label string) float64 {
	return testutil.ToFloat64(c.WithLabelValues(label))
}

// Counters an alert reads with increase() have every series at 0 before the
// first event, or the first event would read as no increase.
func TestNewMetrics_AlertedCountersStartAtZero(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	assert.Equal(t, 2, testutil.CollectAndCount(m.ExternalChanges))
	assert.Equal(t, 3, testutil.CollectAndCount(m.RefusedPasses))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.RefusedPasses.WithLabelValues(RefusedEmpty)))
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
	assert.Equal(t, []ListItem{{IP: "45.1.1.1", Comment: "dz1"}, {IP: "45.1.1.9", Comment: "dz1"}, {IP: "45.2.2.2", Comment: "dz2"}}, h.cfItems())
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

	// Only a device code changes: the map is keyed on pubkeys, and Cloudflare
	// keeps the old comment rather than send an address it already holds.
	h.ledger.pd.Devices[3].Code = "dz9-renamed"
	require.NoError(t, h.pass(t))
	cmUpdates, cfReplaces = h.wrote()
	assert.Equal(t, [2]int{2, 2}, [2]int{cmUpdates, cfReplaces})
	assert.Contains(t, h.cfItems(), ListItem{IP: "45.0.0.9", Comment: "dz9"})
}

// A failed ledger or exception file read writes nothing. A target that cannot
// be read is skipped, and the other target is still written.
func TestPass_ReadFailures(t *testing.T) {
	malformed := dev(9, "dz9", serviceability.DeviceStatusActivated, "45.0.0.9")
	malformed.DeserializeError = errors.New("trailing interfaces length mismatch")

	cases := map[string]struct {
		breakIt func(h *harness)
		stage   string
		err     string
		writes  [2]int // map updates, Cloudflare writes
	}{
		"ledger error": {func(h *harness) { h.ledger.err = errors.New("rpc down") }, "ledger", "ledger: rpc down", [2]int{}},
		"no devices": {func(h *harness) { h.ledger.pd.Devices = nil }, "ledger",
			"ledger: incomplete ledger read: no device accounts", [2]int{}},
		"no global state": {func(h *harness) { h.ledger.pd.GlobalState = nil }, "ledger",
			"ledger: incomplete ledger read: no global state account", [2]int{}},
		"malformed device": {func(h *harness) { h.ledger.pd.Devices = append(h.ledger.pd.Devices, malformed) }, "ledger",
			"ledger: incomplete ledger read: device " + pk(9) + " (dz9) did not deserialize: trailing interfaces length mismatch", [2]int{}},
		"exception file missing": {func(h *harness) { h.cfg.ExceptionsFile = "/nonexistent/exceptions.yaml" }, "exceptions",
			"exceptions: reading exception file: open /nonexistent/exceptions.yaml: no such file or directory", [2]int{}},
		"configmap read": {func(h *harness) { h.cm.getErr = errors.New("forbidden") }, "read_gnmic_map", "read_gnmic_map: forbidden",
			[2]int{0, 1}},
		"configmap absent": {func(h *harness) { h.cm.cm = nil }, "read_gnmic_map", "read_gnmic_map: configmap not found",
			[2]int{0, 1}},
		"configmap garbage": {func(h *harness) { h.cm.cm.Data = map[string]string{"map.json": "{"} }, "read_gnmic_map",
			`read_gnmic_map: configmap key "map.json" holds no address map, so the sync cannot tell what a write would remove; fix or empty the key: unexpected end of JSON input`,
			[2]int{0, 1}},
		"cloudflare read": {func(h *harness) { h.cf.itemsErr = errors.New("HTTP 403") }, "read_cloudflare_list", "read_cloudflare_list: HTTP 403",
			[2]int{1, 0}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, fleet(3))
			tc.breakIt(h)
			err := h.pass(t)
			assert.EqualError(t, err, tc.err)
			cmUpdates, cfReplaces := h.wrote()
			assert.Equal(t, tc.writes, [2]int{cmUpdates, cfReplaces})
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

// Duplicate items do not widen the removal limit: it is measured against the
// distinct addresses. 3 of 23 is 13%, though 3 of 30 items would be 10%.
func TestPass_RemovalLimitCountsCloudflareAddressesOnce(t *testing.T) {
	h := newHarness(t, fleet(20))
	require.NoError(t, h.pass(t))
	for i := range 3 {
		h.cf.items = append(h.cf.items, ListItem{IP: fmt.Sprintf("45.9.9.%d", i+1)})
	}
	for i := range 7 {
		h.cf.items = append(h.cf.items, ListItem{IP: fmt.Sprintf("45.0.0.%d", i+1)})
	}
	var refused *RefusedError
	require.ErrorAs(t, h.pass(t), &refused)
	assert.Equal(t, RefusedTooManyAddrs, refused.Reason)
	assert.Contains(t, refused.Detail, "cloudflare_list would remove 3 of 23 addresses")
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

// A refused pass still writes the list's additions to both targets and holds
// every removal, so one contributor retiring devices does not hold back
// another's new device. The override then applies the removals.
func TestPass_RefusedPassWritesAdditionsAndHoldsRemovals(t *testing.T) {
	h := newHarness(t, fleet(60))
	require.NoError(t, h.pass(t))
	t0 := testutil.ToFloat64(h.metrics.LastSuccess)
	h.clock.Advance(time.Minute)
	h.ledger.pd.Devices = append(h.ledger.pd.Devices[:54:54], dev(61, "dz61", serviceability.DeviceStatusActivated, "45.0.0.61"))

	var refused *RefusedError
	require.ErrorAs(t, h.pass(t), &refused)
	assert.Equal(t, RefusedTooManyDevices, refused.Reason)
	assert.Contains(t, refused.Detail, "additions are written and every removal is held")
	assert.Equal(t, 1.0, counter(h.metrics.RefusedPasses, RefusedTooManyDevices))
	assert.Equal(t, t0, testutil.ToFloat64(h.metrics.LastSuccess), "a refused pass is not a success")
	m := h.cm.gnmicMap(t)
	assert.Len(t, m, 61)
	assert.Equal(t, []string{"45.0.0.61"}, m[pk(61)])
	assert.Equal(t, []string{"45.0.0.60"}, m[pk(60)], "a device the ledger dropped keeps its entry")
	assert.Len(t, h.cfIPs(), 61)
	assert.Contains(t, h.cfIPs(), "45.0.0.61")
	cmUpdates, cfWrites := h.wrote()
	assert.Equal(t, [2]int{2, 2}, [2]int{cmUpdates, cfWrites})

	// The next pass has nothing to add: it writes nothing, is refused again,
	// and does not count its own held write as an outside edit.
	require.ErrorAs(t, h.pass(t), &refused)
	cmUpdates, cfWrites = h.wrote()
	assert.Equal(t, [2]int{2, 2}, [2]int{cmUpdates, cfWrites})
	assert.Equal(t, 2.0, counter(h.metrics.RefusedPasses, RefusedTooManyDevices))
	assert.Equal(t, 0.0, h.external("gnmic_map"))
	assert.Equal(t, 0.0, h.external("cloudflare_list"))

	h.cfg.AllowShrinkTo = mustBuild(t, h.ledger.pd.Devices, Exceptions{}).List.Digest()
	require.NoError(t, h.pass(t))
	assert.Len(t, h.cm.gnmicMap(t), 55)
	assert.Len(t, h.cfIPs(), 55)
}

// Stray Cloudflare items past the address limit refuse every pass, and a new
// device still reaches both targets while the strays stay.
func TestPass_StrayItemsDoNotHoldBackANewDevice(t *testing.T) {
	h := newHarness(t, fleet(10))
	require.NoError(t, h.pass(t))
	for i := range 3 {
		h.cf.items = append(h.cf.items, ListItem{IP: fmt.Sprintf("45.9.9.%d", i+1)})
	}
	h.ledger.pd.Devices = append(h.ledger.pd.Devices, dev(11, "dz11", serviceability.DeviceStatusActivated, "45.0.0.11"))

	var refused *RefusedError
	require.ErrorAs(t, h.pass(t), &refused)
	assert.Equal(t, RefusedTooManyAddrs, refused.Reason)
	assert.Contains(t, h.cfIPs(), "45.0.0.11")
	assert.Contains(t, h.cfIPs(), "45.9.9.1")
	assert.Len(t, h.cfIPs(), 14)
	assert.Equal(t, []string{"45.0.0.11"}, h.cm.gnmicMap(t)[pk(11)])
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
	h.callers.calls = []Call{{DevicePubkey: pk(1), CallerIP: netip.MustParseAddr("198.51.100.8"), LastSeen: t0}}
	require.NoError(t, h.pass(t))
	assert.Equal(t, t0.Add(-24*time.Hour), h.callers.since)
	offchain := `
# HELP doublezero_telemetry_allowlist_sync_offchain_caller_last_seen_timestamp_seconds Newest call per device and calling address the ledger does not hold for that device.
# TYPE doublezero_telemetry_allowlist_sync_offchain_caller_last_seen_timestamp_seconds gauge
doublezero_telemetry_allowlist_sync_offchain_caller_last_seen_timestamp_seconds{caller_ip="198.51.100.8",code="dz1",listed="false",pubkey="` + pk(1) + `"} 1.7939664e+09
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

// A caller read cut at its limit still reports what it kept, and says so.
func TestPass_CutCallerReadStillReports(t *testing.T) {
	h := newHarness(t, fleet(2))
	h.callers.calls = []Call{{DevicePubkey: pk(1), CallerIP: netip.MustParseAddr("198.51.100.8"), LastSeen: t0}}
	h.callers.cut = true
	require.NoError(t, h.pass(t))
	assert.Equal(t, 1.0, testutil.ToFloat64(h.metrics.CallerReadCut))
	assert.Equal(t, 0.0, counter(h.metrics.Errors, "report"))
	assert.Equal(t, float64(t0.Unix()), testutil.ToFloat64(h.metrics.ReportLastSuccess), "the report is fresh")
	assert.Equal(t, 1, testutil.CollectAndCount(h.metrics.OffchainCaller), "the flood's caller is reported")

	h.clock.Advance(time.Minute)
	h.callers.cut = false
	require.NoError(t, h.pass(t))
	assert.Equal(t, 0.0, testutil.ToFloat64(h.metrics.CallerReadCut))
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
	cfg.ExceptionsFile = h.cfg.ExceptionsFile
	cfg.PassTimeout = DefaultReportTimeout
	_, err = New(cfg)
	assert.EqualError(t, err, "pass timeout 20s must be longer than the report timeout 20s")
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

// fleet3 returns n Activated devices with three addresses each.
func fleet3(n int) []serviceability.Device {
	out := make([]serviceability.Device, n)
	for i := range out {
		out[i] = dev(byte(i+1), fmt.Sprintf("dz%d", i+1), serviceability.DeviceStatusActivated,
			fmt.Sprintf("45.0.%d.1", i+1), fmt.Sprintf("45.1.%d.1/31", i+1), fmt.Sprintf("45.2.%d.1/32", i+1))
	}
	return out
}

// A device still live but left with no global address is refused by gnmic as
// surely as one that is gone, so it counts toward the device limit. Emptying 8
// of 100 three-address devices removes 8% of addresses, under the address limit.
func TestPass_DeviceLeftWithNoAddressCountsAsRemoved(t *testing.T) {
	h := newHarness(t, fleet3(100))
	require.NoError(t, h.pass(t))
	for i := 0; i < MaxRemovedDevices+3; i++ {
		d := &h.ledger.pd.Devices[i]
		d.PublicIp = ip4("10.0.0.1")
		d.Interfaces = nil
	}
	var refused *RefusedError
	require.ErrorAs(t, h.pass(t), &refused)
	assert.Equal(t, RefusedTooManyDevices, refused.Reason)
	assert.Equal(t, []string{"45.0.1.1", "45.1.1.1", "45.2.1.1"}, h.cm.gnmicMap(t)[pk(1)], "the map keeps the device's addresses")
}

type hungCallers struct{}

// ctxConfigMap fails a read once its context is done, as the Kubernetes client does.
type ctxConfigMap struct{ *fakeConfigMap }

func (c ctxConfigMap) Get(ctx context.Context) (*ConfigMap, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.fakeConfigMap.Get(ctx)
}

func (hungCallers) RecentCalls(ctx context.Context, _ time.Time) ([]Call, bool, error) {
	<-ctx.Done()
	return nil, false, ctx.Err()
}

// A ClickHouse that stalls costs the report, never the list.
func TestPass_HungCallerReadDoesNotStopTheList(t *testing.T) {
	h := newHarness(t, fleet(3))
	h.cfg.Callers = hungCallers{}
	h.cfg.ReportTimeout = 50 * time.Millisecond
	cfg := h.cfg
	cfg.Targets = []Target{&GnmicMapTarget{API: ctxConfigMap{h.cm}, Key: "map.json"}, &CloudflareTarget{API: h.cf}}
	s, err := New(cfg)
	require.NoError(t, err)
	// The pass deadline is longer than the report's: without the report's own
	// deadline the stalled read would use up the pass and the map read would fail.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, s.Pass(ctx))
	cmWrites, cfWrites := h.wrote()
	assert.Equal(t, 1, cmWrites)
	assert.Equal(t, 1, cfWrites)
	assert.Equal(t, float64(1), counter(h.metrics.Errors, "report"))
}

// An unlisted caller is logged once while it keeps calling, not on every pass.
func TestPass_UnlistedCallerLoggedOnce(t *testing.T) {
	h := newHarness(t, fleet(1))
	h.callers.calls = []Call{{DevicePubkey: pk(1), CallerIP: netip.MustParseAddr("198.18.0.1"), LastSeen: t0}}
	var buf bytes.Buffer
	h.cfg.Log = slog.New(slog.NewTextHandler(&buf, nil))
	cfg := h.cfg
	cfg.Targets = []Target{&GnmicMapTarget{API: h.cm, Key: "map.json"}, &CloudflareTarget{API: h.cf}}
	s, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, s.Pass(context.Background()))
	require.NoError(t, s.Pass(context.Background()))
	assert.Equal(t, 1, strings.Count(buf.String(), "device calls from an address not on its entry"))
}

// A change adds what is missing and then deletes what left, by id: an
// address that stays on the list is never deleted or sent again, even when
// its comment no longer matches. It holds whichever way Cloudflare treats an
// address it already has.
func TestPass_CloudflareAddsThenDeletesAndKeepsWhatStays(t *testing.T) {
	for _, appendExisting := range []bool{false, true} {
		t.Run(fmt.Sprintf("appendExisting=%v", appendExisting), func(t *testing.T) {
			testCloudflareAddsThenDeletes(t, appendExisting)
		})
	}
}

func testCloudflareAddsThenDeletes(t *testing.T, appendExisting bool) {
	devices := fleet(20)
	h := newHarness(t, devices)
	h.cf.appendExisting = appendExisting
	require.NoError(t, h.pass(t))
	idOf := func(ip string) string {
		for _, it := range h.cf.items {
			if it.IP == ip {
				return it.ID
			}
		}
		return ""
	}
	keepID, goneID := idOf("45.0.0.1"), idOf("45.0.0.2")
	require.NotEmpty(t, keepID)
	h.cf.ops = nil

	// dz1 is renamed (a comment change), dz2 moves to a new address.
	devices[0].Code = "dz1-renamed"
	devices[1].PublicIp = [4]uint8{45, 0, 1, 2}
	require.NoError(t, h.pass(t))

	assert.Equal(t, []string{"add 45.0.1.2", "delete " + goneID}, h.cf.ops)
	assert.Equal(t, keepID, idOf("45.0.0.1"), "an address that stays keeps its item")
	assert.Contains(t, h.cfItems(), ListItem{IP: "45.0.0.1", Comment: "dz1"}, "a stale comment is left as it is")
	assert.Len(t, h.cf.items, 20)
	h.cf.ops = nil
	require.NoError(t, h.pass(t))
	assert.Empty(t, h.cf.ops, "a stale comment alone writes nothing")
	assert.Equal(t, 0.0, h.external("cloudflare_list"), "a comment the sync left is not an outside edit")
}

// A list holding two items for one address converges to the oldest, and
// never loses the address. The extra items are not removed addresses, so
// they do not count against the removal limits.
func TestPass_CloudflareDuplicatesKeepTheOldest(t *testing.T) {
	h := newHarness(t, fleet(3))
	h.cf.items = []ListItem{
		{ID: "newer", IP: "45.0.0.1", Comment: "dz1", CreatedOn: "2026-02-01T00:00:00Z"},
		{ID: "older", IP: "45.0.0.1/32", Comment: "by hand", CreatedOn: "2026-01-01T00:00:00Z"},
		{ID: "newest", IP: "45.0.0.1", CreatedOn: "2026-03-01T00:00:00Z"},
		{ID: "two", IP: "45.0.0.2", Comment: "dz2", CreatedOn: "2026-01-01T00:00:00Z"},
		{ID: "three", IP: "45.0.0.3", Comment: "dz3", CreatedOn: "2026-01-01T00:00:00Z"},
	}
	require.NoError(t, h.pass(t))
	assert.Equal(t, []string{"delete newer,newest"}, h.cf.ops)
	ids := []string{}
	for _, it := range h.cf.items {
		ids = append(ids, it.ID)
	}
	assert.ElementsMatch(t, []string{"older", "two", "three"}, ids)
	h.cf.ops = nil
	require.NoError(t, h.pass(t))
	assert.Empty(t, h.cf.ops)
	assert.Equal(t, 0.0, h.external("cloudflare_list"), "the sync's own dedup is not an outside edit")
}

// A pass that finds the list changed since the previous pass left it counts
// an outside edit, once, whatever it then writes.
func TestPass_CountsCloudflareEditsTheSyncDidNotMake(t *testing.T) {
	h := newHarness(t, fleet(20))
	ext := func() float64 { return h.external("cloudflare_list") }

	// The first pass has no previous pass to compare with.
	h.cf.items = []ListItem{{IP: "45.0.0.1", Comment: "set by hand"}}
	require.NoError(t, h.pass(t))
	assert.Equal(t, 0.0, ext())
	require.NoError(t, h.pass(t))
	assert.Equal(t, 0.0, ext(), "the sync's own write is not an outside edit")

	// Someone adds an item: counted, and the sync removes it.
	h.cf.items = append(h.cf.items, ListItem{IP: "45.9.9.9", Comment: "by hand"})
	require.NoError(t, h.pass(t))
	assert.Equal(t, 1.0, ext())
	assert.NotContains(t, h.cfIPs(), "45.9.9.9")
	require.NoError(t, h.pass(t))
	assert.Equal(t, 1.0, ext(), "the next pass finds the list as the sync left it")

	// Someone deletes an item, or edits a comment: each counts.
	h.cf.items = h.cf.items[1:]
	require.NoError(t, h.pass(t))
	assert.Equal(t, 2.0, ext())
	h.cf.items[0].Comment = "edited"
	require.NoError(t, h.pass(t))
	assert.Equal(t, 3.0, ext())

	// A refused pass still counts the edit it found.
	for i := range 5 {
		h.cf.items = append(h.cf.items, ListItem{IP: fmt.Sprintf("45.9.8.%d", i+1)})
	}
	var refused *RefusedError
	require.ErrorAs(t, h.pass(t), &refused)
	assert.Equal(t, 4.0, ext())
	assert.Equal(t, 0.0, h.external("gnmic_map"), "each target counts its own edits")
}

// gnmic's map is counted the same way: by what gnmic would read, per pubkey.
func TestPass_CountsGnmicMapEditsTheSyncDidNotMake(t *testing.T) {
	devices := fleet(20)
	devices[0].AgentIp = [4]uint8{45, 0, 2, 1}
	h := newHarness(t, devices)
	require.NoError(t, h.pass(t))
	require.NoError(t, h.pass(t))
	assert.Equal(t, 0.0, h.external("gnmic_map"))

	edit := func(f func(m GnmicAddressMap)) {
		m := h.cm.gnmicMap(t)
		f(m)
		raw, err := RenderGnmicMap(m)
		require.NoError(t, err)
		h.cm.cm.Data["map.json"] = raw
	}
	// An operator adds an address to a device's entry.
	edit(func(m GnmicAddressMap) { m[pk(1)] = append(m[pk(1)], "45.9.9.9") })
	require.NoError(t, h.pass(t))
	assert.Equal(t, 1.0, h.external("gnmic_map"))
	assert.Equal(t, []string{"45.0.0.1", "45.0.2.1"}, h.cm.gnmicMap(t)[pk(1)], "the sync puts the entry back")
	require.NoError(t, h.pass(t))
	assert.Equal(t, 1.0, h.external("gnmic_map"))

	// Reordering a device's addresses is not an edit: gnmic reads a set.
	edit(func(m GnmicAddressMap) { slices.Reverse(m[pk(1)]) })
	require.NoError(t, h.pass(t))
	assert.Equal(t, 1.0, h.external("gnmic_map"))

	// A formatting change gnmic reads the same is not an edit.
	h.cm.cm.Data["map.json"] = strings.ReplaceAll(h.cm.cm.Data["map.json"], "\n  ", "\n    ")
	require.NoError(t, h.pass(t))
	assert.Equal(t, 1.0, h.external("gnmic_map"))

	// Nor is repeating an address the entry already holds.
	edit(func(m GnmicAddressMap) { m[pk(1)] = append(m[pk(1)], m[pk(1)][0]) })
	require.NoError(t, h.pass(t))
	assert.Equal(t, 1.0, h.external("gnmic_map"))

	// An operator deletes a device's entry.
	edit(func(m GnmicAddressMap) { delete(m, pk(2)) })
	require.NoError(t, h.pass(t))
	assert.Equal(t, 2.0, h.external("gnmic_map"))
	assert.Equal(t, 0.0, h.external("cloudflare_list"))
}

// After a start the sync remembers nothing, so its first pass counts no
// outside edit, whatever it finds and whatever it writes. The pass after it
// compares again.
func TestPass_FirstPassAfterRestartCountsNoOutsideEdit(t *testing.T) {
	h := newHarness(t, fleet(20))
	require.NoError(t, h.pass(t))

	h.restart()
	h.cf.items = append(h.cf.items, ListItem{IP: "45.9.9.9"})
	h.cm.cm.Data["map.json"] = strings.Replace(h.cm.cm.Data["map.json"], `"45.0.0.1"`, `"45.0.0.1", "45.9.9.9"`, 1)
	require.NoError(t, h.pass(t))
	assert.Equal(t, [2]float64{0, 0}, [2]float64{h.external("gnmic_map"), h.external("cloudflare_list")})
	assert.NotContains(t, h.cfIPs(), "45.9.9.9", "the first pass still writes the list")

	h.cf.items = append(h.cf.items, ListItem{IP: "45.9.9.8"})
	require.NoError(t, h.pass(t))
	assert.Equal(t, [2]float64{0, 1}, [2]float64{h.external("gnmic_map"), h.external("cloudflare_list")})
}

// An update that returns an error may have been committed: a timeout after
// the API server applied it. The next pass must not count the sync's own
// write as an outside edit.
func TestPass_ErroredGnmicMapWriteIsNotAnOutsideEdit(t *testing.T) {
	devices := fleet(20)
	h := newHarness(t, devices)
	require.NoError(t, h.pass(t))
	devices[1].PublicIp = [4]uint8{45, 0, 1, 2}
	h.cm.updateErr, h.cm.commitThenErr = context.DeadlineExceeded, true
	require.Error(t, h.pass(t))
	h.cm.updateErr, h.cm.commitThenErr = nil, false
	require.NoError(t, h.pass(t))
	assert.Equal(t, 0.0, h.external("gnmic_map"))

	// The pass after that compares again.
	h.cm.cm.Data["map.json"] = strings.Replace(h.cm.cm.Data["map.json"], `"45.0.0.1"`, `"45.0.0.1", "45.9.9.9"`, 1)
	require.NoError(t, h.pass(t))
	assert.Equal(t, 1.0, h.external("gnmic_map"))
}

func TestCloudflareItems_TruncatesLongComments(t *testing.T) {
	list := List{Devices: map[string]Device{}}
	for i := range 200 {
		list.Devices[pk(byte(i+1))] = Device{Code: fmt.Sprintf("dev%03d-é", i), Onchain: addrs("45.0.0.1")}
	}
	items := cloudflareItems(list)
	require.Len(t, items, 1)
	assert.LessOrEqual(t, len(items[0].Comment), maxCommentBytes)
	assert.Greater(t, len(items[0].Comment), maxCommentBytes-2)
	assert.True(t, utf8.ValidString(items[0].Comment), "cut at a character boundary")
	assert.Equal(t, "short", truncateComment("short"))
	// Byte 500 falls inside the two-byte é, so the cut moves back before it.
	assert.Equal(t, strings.Repeat("a", 499), truncateComment(strings.Repeat("a", 499)+"é"))
}

// In dry run the sync writes nothing, so every change between passes is an
// outside edit.
func TestPass_DryRunCountsCloudflareEdits(t *testing.T) {
	h := newHarness(t, fleet(20))
	require.NoError(t, h.pass(t))
	h.cf.ops = nil
	h.cfg.DryRun = true
	require.NoError(t, h.pass(t))
	assert.Equal(t, 0.0, h.external("cloudflare_list"))
	h.cf.items = append(h.cf.items, ListItem{IP: "45.9.9.9"})
	require.NoError(t, h.pass(t))
	assert.Equal(t, 1.0, h.external("cloudflare_list"))
	assert.Empty(t, h.cf.ops)
}

// A write that fails partway leaves a list no pass recorded, so the next pass
// does not count what it finds as an outside edit.
func TestPass_PartialCloudflareWriteIsNotAnOutsideEdit(t *testing.T) {
	devices := fleet(20)
	h := newHarness(t, devices)
	require.NoError(t, h.pass(t))
	devices[1].PublicIp = [4]uint8{45, 0, 1, 2}
	h.cf.deleteErr = errors.New("HTTP 500")
	require.Error(t, h.pass(t))
	assert.Equal(t, []string{"add 45.0.1.2"}, h.cf.ops[len(h.cf.ops)-1:], "the add landed before the delete failed")
	h.cf.deleteErr = nil
	require.NoError(t, h.pass(t))
	assert.Equal(t, 0.0, h.external("cloudflare_list"))
	assert.NotContains(t, h.cfIPs(), "45.0.0.2")
}

func TestCloudflareTarget_RefusesToEmptyTheList(t *testing.T) {
	cf := &fakeIPList{items: []ListItem{{IP: "45.0.0.1"}}}
	target := &CloudflareTarget{API: cf}
	p, err := target.Plan(context.Background(), List{})
	require.NoError(t, err)
	assert.EqualError(t, target.Apply(context.Background(), p), "refusing to empty the cloudflare list")
	assert.Empty(t, cf.ops)
}
