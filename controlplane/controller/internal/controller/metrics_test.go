package controller

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// countSeriesWithLabel collects a metric vector directly (bypassing the global
// registry to avoid interference from other tests) and counts the series whose
// label `name` equals `value`. Tests use unique pubkeys so the count is exact.
func countSeriesWithLabel(c prometheus.Collector, name, value string) int {
	ch := make(chan prometheus.Metric, 1024)
	c.Collect(ch)
	close(ch)
	n := 0
	for m := range ch {
		d := &dto.Metric{}
		if err := m.Write(d); err != nil {
			continue
		}
		for _, lp := range d.GetLabel() {
			if lp.GetName() == name && lp.GetValue() == value {
				n++
				break
			}
		}
	}
	return n
}

// seedDeviceMetrics registers a series carrying the given device pubkey in each
// of the per-device metric vectors that deleteDeviceMetrics is expected to prune.
func seedDeviceMetrics(pubkey, code string) {
	getConfigOps.WithLabelValues(pubkey, code, "contrib", "exch", "loc", "Activated", "v1", "abc", "2026-01-01").Inc()
	getConfigRenderErrors.WithLabelValues(pubkey).Inc()
	duplicateTunnelPairs.WithLabelValues(pubkey, code).Inc()
	linkMetrics.WithLabelValues(code, "Ethernet1", pubkey).Set(1)
}

func TestSwapCache_PrunesRemovedDeviceMetrics(t *testing.T) {
	const (
		removed   = "PrunePubkeyRemoved111111111111111111111111"
		surviving = "PrunePubkeySurviving22222222222222222222222"
	)
	seedDeviceMetrics(removed, "dev-removed")
	seedDeviceMetrics(surviving, "dev-surviving")

	// Sanity: both pubkeys have series before the swap.
	for _, pk := range []string{removed, surviving} {
		if got := countSeriesWithLabel(getConfigOps, "pubkey", pk); got != 1 {
			t.Fatalf("setup: expected 1 getConfigOps series for %s, got %d", pk, got)
		}
		if got := countSeriesWithLabel(linkMetrics, "device_pubkey", pk); got != 1 {
			t.Fatalf("setup: expected 1 linkMetrics series for %s, got %d", pk, got)
		}
	}

	c := &Controller{
		log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		cache: stateCache{Devices: map[string]*Device{
			removed:   {PubKey: removed, Code: "dev-removed"},
			surviving: {PubKey: surviving, Code: "dev-surviving"},
		}},
	}

	// New cache no longer contains the removed device.
	c.swapCache(stateCache{Devices: map[string]*Device{
		surviving: {PubKey: surviving, Code: "dev-surviving"},
	}})

	// The removed pubkey's series are gone from every per-device vector.
	for _, tc := range []struct {
		name  string
		vec   prometheus.Collector
		label string
	}{
		{"getConfigOps", getConfigOps, "pubkey"},
		{"getConfigRenderErrors", getConfigRenderErrors, "pubkey"},
		{"duplicateTunnelPairs", duplicateTunnelPairs, "pubkey"},
		{"linkMetrics", linkMetrics, "device_pubkey"},
	} {
		if got := countSeriesWithLabel(tc.vec, tc.label, removed); got != 0 {
			t.Errorf("%s: expected removed pubkey series to be pruned, got %d", tc.name, got)
		}
		if got := countSeriesWithLabel(tc.vec, tc.label, surviving); got != 1 {
			t.Errorf("%s: expected surviving pubkey series to be untouched, got %d", tc.name, got)
		}
	}
}

// recordingHandler is a minimal slog.Handler that captures emitted records so a
// test can assert on level and attributes.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func TestGetConfig_LedgerAbsentPubkey(t *testing.T) {
	const absent = "AbsentPubkeyNeverInLedger3333333333333333333"

	handler := &recordingHandler{}
	c := &Controller{
		log:   slog.New(handler),
		cache: stateCache{Devices: map[string]*Device{}},
	}

	before := testutil.ToFloat64(getConfigUnknownPubkey)

	// Attach a gRPC peer so the warning can record the caller's address.
	const peerAddr = "203.0.113.7:51234"
	ctx := peer.NewContext(context.Background(), &peer.Peer{
		Addr: &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51234},
	})

	// Call twice in quick succession: the aggregate counter must count every
	// call, but the warning is rate-limited so only one is emitted per minute.
	for i := 0; i < 2; i++ {
		_, err := c.GetConfig(ctx, &pb.ConfigRequest{Pubkey: absent})
		// Returns the existing not-found error path.
		if status.Code(err) != codes.NotFound {
			t.Fatalf("expected NotFound, got %v", err)
		}
	}

	// No per-pubkey getConfigOps series is created for an absent device.
	if got := countSeriesWithLabel(getConfigOps, "pubkey", absent); got != 0 {
		t.Errorf("expected no getConfigOps series for absent pubkey, got %d", got)
	}

	// The low-cardinality aggregate counter incremented once per call.
	if got := testutil.ToFloat64(getConfigUnknownPubkey) - before; got != 2 {
		t.Errorf("expected getConfigUnknownPubkey to increase by 2, got %v", got)
	}

	// Exactly one rate-limited WARN log naming the device pubkey and the caller's
	// address was emitted.
	warned := 0
	for _, r := range handler.records {
		if r.Level != slog.LevelWarn {
			continue
		}
		var gotPubkey, gotPeer string
		r.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "device_pubkey":
				gotPubkey = a.Value.String()
			case "peer":
				gotPeer = a.Value.String()
			}
			return true
		})
		if gotPubkey != absent {
			continue
		}
		warned++
		if gotPeer != peerAddr {
			t.Errorf("expected WARN log peer=%s, got %q", peerAddr, gotPeer)
		}
	}
	if warned != 1 {
		t.Errorf("expected exactly 1 rate-limited WARN log with device_pubkey=%s, got %d", absent, warned)
	}
}

func TestGetConfig_PrunedPubkeyNotResurrected(t *testing.T) {
	const (
		gone      = "NoResurrectPubkeyGone4444444444444444444444"
		stillHere = "NoResurrectPubkeyHere5555555555555555555555"
	)
	seedDeviceMetrics(gone, "dev-gone")

	c := &Controller{
		log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		cache: stateCache{Devices: map[string]*Device{
			gone: {PubKey: gone, Code: "dev-gone"},
		}},
	}

	// Remove the device from the ledger; its series are pruned.
	c.swapCache(stateCache{Devices: map[string]*Device{
		stillHere: {PubKey: stillHere, Code: "dev-still-here"},
	}})
	if got := countSeriesWithLabel(getConfigOps, "pubkey", gone); got != 0 {
		t.Fatalf("expected getConfigOps series pruned after swap, got %d", got)
	}

	// The dead box keeps calling in with its old pubkey.
	for i := 0; i < 3; i++ {
		if _, err := c.GetConfig(context.Background(), &pb.ConfigRequest{Pubkey: gone}); status.Code(err) != codes.NotFound {
			t.Fatalf("expected NotFound for pruned pubkey, got %v", err)
		}
	}

	// The per-pubkey getConfigOps series the alert reads stays absent.
	if got := countSeriesWithLabel(getConfigOps, "pubkey", gone); got != 0 {
		t.Errorf("expected getConfigOps series to stay absent after repeated calls, got %d", got)
	}
}
