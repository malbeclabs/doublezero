package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_Pinger(t *testing.T) {
	t.Parallel()

	newPK := func(b byte) solana.PublicKey {
		var pk solana.PublicKey
		pk[0] = b
		return pk
	}

	t.Run("records successful RTT sample", func(t *testing.T) {
		t.Parallel()

		epoch := uint64(100)
		devicePK := newPK(1)
		peerPK := newPK(2)
		linkPK := newPK(3)

		mockPeers := newMockPeerDiscovery()
		mockPeers.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: "tun1-2",
					SourceIP:  ipv4([4]uint8{127, 0, 0, 1}),
					TargetIP:  ipv4([4]uint8{127, 0, 0, 2}),
				},
			},
		})

		mockSender := &mockSender{rtt: 42 * time.Millisecond}
		getSender := func(_ context.Context, _ *telemetry.Peer) twamplight.Sender { return mockSender }

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			LocalDevicePK: devicePK,
			Peers:         mockPeers,
			Buffer:        buffer,
			GetSender:     getSender,
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return epoch, nil
			},
		})

		pinger.Tick(context.Background())

		samples := buffer.FlushWithoutReset()
		key := telemetry.PartitionKey{
			OriginDevicePK: devicePK,
			TargetDevicePK: peerPK,
			LinkPK:         linkPK,
			Epoch:          epoch,
		}

		s, ok := samples[key]
		require.True(t, ok, "expected sample under account key")
		require.Len(t, s, 1)
		assert.False(t, s[0].Loss)
		assert.Equal(t, 42*time.Millisecond, s[0].RTT)
	})

	t.Run("records loss when tunnel is nil", func(t *testing.T) {
		t.Parallel()

		devicePK := newPK(4)
		peerPK := newPK(5)
		linkPK := newPK(6)

		mockPeers := newMockPeerDiscovery()
		mockPeers.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel:   nil,
			},
		})

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			LocalDevicePK: devicePK,
			Peers:         mockPeers,
			Buffer:        buffer,
			GetSender:     func(_ context.Context, _ *telemetry.Peer) twamplight.Sender { return nil },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})

		pinger.Tick(context.Background())

		samples := buffer.FlushWithoutReset()
		var found bool
		for key, val := range samples {
			if key.OriginDevicePK == devicePK && key.TargetDevicePK == peerPK && key.LinkPK == linkPK {
				require.Len(t, val, 1)
				assert.True(t, val[0].Loss)
				assert.Zero(t, val[0].RTT)
				found = true
			}
		}
		assert.True(t, found, "expected loss sample for peer")
	})

	t.Run("records loss when sender is nil", func(t *testing.T) {
		t.Parallel()

		devicePK := newPK(4)
		peerPK := newPK(5)
		linkPK := newPK(6)

		mockPeers := newMockPeerDiscovery()
		mockPeers.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: "tun1-2",
					SourceIP:  ipv4([4]uint8{127, 0, 0, 1}),
					TargetIP:  ipv4([4]uint8{127, 0, 0, 2}),
				},
			},
		})

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			LocalDevicePK: devicePK,
			Peers:         mockPeers,
			Buffer:        buffer,
			GetSender:     func(_ context.Context, _ *telemetry.Peer) twamplight.Sender { return nil },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})

		pinger.Tick(context.Background())

		samples := buffer.FlushWithoutReset()
		var found bool
		for key, val := range samples {
			if key.OriginDevicePK == devicePK && key.TargetDevicePK == peerPK && key.LinkPK == linkPK {
				require.Len(t, val, 1)
				assert.True(t, val[0].Loss)
				assert.Zero(t, val[0].RTT)
				found = true
			}
		}
		assert.True(t, found, "expected loss sample for peer2")
	})

	t.Run("records loss on sender error", func(t *testing.T) {
		t.Parallel()

		devicePK := newPK(7)
		peerPK := newPK(8)
		linkPK := newPK(9)

		mockPeers := newMockPeerDiscovery()
		mockPeers.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: "tun1-2",
					SourceIP:  ipv4([4]uint8{127, 0, 0, 1}),
					TargetIP:  ipv4([4]uint8{127, 0, 0, 2}),
				},
			},
		})

		mockSender := &mockSender{err: errors.New("mock failure")}
		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			LocalDevicePK: devicePK,
			Peers:         mockPeers,
			Buffer:        buffer,
			GetSender:     func(_ context.Context, _ *telemetry.Peer) twamplight.Sender { return mockSender },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})

		pinger.Tick(context.Background())

		samples := buffer.FlushWithoutReset()
		var found bool
		for key, val := range samples {
			if key.OriginDevicePK == devicePK && key.TargetDevicePK == peerPK && key.LinkPK == linkPK {
				require.Len(t, val, 1)
				assert.True(t, val[0].Loss)
				assert.Zero(t, val[0].RTT)
				found = true
			}
		}
		assert.True(t, found, "expected loss sample for peer3")
	})

	t.Run("retries getCurrentEpoch before succeeding", func(t *testing.T) {
		t.Parallel()

		devicePK := newPK(10)
		peerPK := newPK(11)
		linkPK := newPK(12)

		attempts := 0
		getCurrentEpoch := func(ctx context.Context) (uint64, error) {
			attempts++
			if attempts < 3 {
				return 0, errors.New("transient failure")
			}
			return 123, nil
		}

		mockPeers := newMockPeerDiscovery()
		mockPeers.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: "tunX",
					SourceIP:  ipv4([4]byte{127, 0, 0, 3}),
					TargetIP:  ipv4([4]byte{127, 0, 0, 4}),
				},
			},
		})

		mockSender := &mockSender{rtt: 7 * time.Millisecond}
		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			LocalDevicePK:   devicePK,
			Peers:           mockPeers,
			Buffer:          buffer,
			GetSender:       func(context.Context, *telemetry.Peer) twamplight.Sender { return mockSender },
			GetCurrentEpoch: getCurrentEpoch,
		})

		pinger.Tick(context.Background())

		assert.Equal(t, 3, attempts, "expected exactly 3 attempts at GetCurrentEpoch")

		samples := buffer.FlushWithoutReset()
		key := telemetry.PartitionKey{
			OriginDevicePK: devicePK,
			TargetDevicePK: peerPK,
			LinkPK:         linkPK,
			Epoch:          123,
		}
		val, ok := samples[key]
		require.True(t, ok, "expected RTT sample for retried epoch")
		require.Len(t, val, 1)
		assert.False(t, val[0].Loss)
		assert.Equal(t, 7*time.Millisecond, val[0].RTT)
	})

	t.Run("tick returns early if getCurrentEpoch exceeds max retries", func(t *testing.T) {
		t.Parallel()

		devicePK := newPK(13)

		var attempts int
		getCurrentEpoch := func(ctx context.Context) (uint64, error) {
			attempts++
			return 0, errors.New("persistent failure")
		}

		mockPeers := newMockPeerDiscovery()
		mockPeers.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: newPK(14),
				LinkPK:   newPK(15),
				Tunnel: &netutil.LocalTunnel{
					Interface: "tunFail",
					SourceIP:  ipv4([4]byte{127, 0, 0, 5}),
					TargetIP:  ipv4([4]byte{127, 0, 0, 6}),
				},
			},
		})

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			LocalDevicePK:   devicePK,
			Peers:           mockPeers,
			Buffer:          buffer,
			GetSender:       func(context.Context, *telemetry.Peer) twamplight.Sender { return nil },
			GetCurrentEpoch: getCurrentEpoch,
		})

		pinger.Tick(context.Background())

		assert.Equal(t, 3, attempts, "should have retried GetCurrentEpoch exactly 3 times")

		samples := buffer.FlushWithoutReset()
		assert.Empty(t, samples, "should not record any samples if epoch retrieval fails")
	})

	// The epoch is only used to build the partition key, so a ledger RPC outage should cost epoch
	// precision, not measurements. See #4125.
	t.Run("probes with the last known epoch while the epoch fetch fails", func(t *testing.T) {
		t.Parallel()

		devicePK, peerPK, linkPK := newPK(20), newPK(21), newPK(22)

		var fetches atomic.Int64
		getCurrentEpoch := func(context.Context) (uint64, error) {
			if fetches.Add(1) == 1 {
				return 100, nil
			}
			return 0, errors.New("ledger rpc unreachable")
		}

		handler := newRecordingHandler()
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.New(handler), &telemetry.PingerConfig{
			LocalDevicePK:        devicePK,
			Interval:             5 * time.Millisecond,
			EpochRefreshInterval: 5 * time.Millisecond,
			Peers:                singleTunnelPeer(t, peerPK, linkPK),
			Buffer:               buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetCurrentEpoch: getCurrentEpoch,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Seed the cache with the only successful fetch, then let the refresh loop fail from here on.
		pinger.Tick(ctx)
		require.Equal(t, int64(1), fetches.Load())

		go func() { _ = pinger.Run(ctx) }()

		key := telemetry.PartitionKey{OriginDevicePK: devicePK, TargetDevicePK: peerPK, LinkPK: linkPK, Epoch: 100}
		require.Eventually(t, func() bool {
			return len(buf.Read(key)) >= 5
		}, 5*time.Second, 5*time.Millisecond, "probing should continue with the cached epoch")

		require.Eventually(t, func() bool {
			return handler.count(slog.LevelWarn, msgEpochFellBack) == 1
		}, 10*time.Second, 10*time.Millisecond, "expected the fallback to be reported")

		cancel()

		assert.Greater(t, fetches.Load(), int64(1), "the refresh loop should keep trying")
		assert.Equal(t, 1, handler.count(slog.LevelWarn, msgEpochFellBack), "fallback should be logged once, not per tick")
		assert.Empty(t, handler.messages(slog.LevelError), "a fallback is not an error")

		// Samples buffer under the cached epoch and nowhere else, ready for the submitter to flush.
		for k := range buf.FlushWithoutReset() {
			assert.Equal(t, uint64(100), k.Epoch)
		}
	})

	t.Run("resumes with the fresh epoch once the ledger recovers", func(t *testing.T) {
		t.Parallel()

		devicePK, peerPK, linkPK := newPK(23), newPK(24), newPK(25)

		var failing atomic.Bool
		var epoch atomic.Uint64
		epoch.Store(100)
		getCurrentEpoch := func(context.Context) (uint64, error) {
			if failing.Load() {
				return 0, errors.New("ledger rpc unreachable")
			}
			return epoch.Load(), nil
		}

		handler := newRecordingHandler()
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.New(handler), &telemetry.PingerConfig{
			LocalDevicePK:        devicePK,
			Interval:             5 * time.Millisecond,
			EpochRefreshInterval: 5 * time.Millisecond,
			Peers:                singleTunnelPeer(t, peerPK, linkPK),
			Buffer:               buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetCurrentEpoch: getCurrentEpoch,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = pinger.Run(ctx) }()

		staleKey := telemetry.PartitionKey{OriginDevicePK: devicePK, TargetDevicePK: peerPK, LinkPK: linkPK, Epoch: 100}
		require.Eventually(t, func() bool {
			return len(buf.Read(staleKey)) >= 1
		}, 5*time.Second, 5*time.Millisecond, "expected samples before the outage")

		failing.Store(true)
		require.Eventually(t, func() bool {
			return handler.count(slog.LevelWarn, msgEpochFellBack) == 1
		}, 10*time.Second, 10*time.Millisecond, "expected the fallback to be reported")

		// Samples keep accumulating under the cached epoch for as long as the outage lasts.
		duringOutage := len(buf.Read(staleKey))
		require.Eventually(t, func() bool {
			return len(buf.Read(staleKey)) > duringOutage+3
		}, 5*time.Second, 5*time.Millisecond, "probing should continue through the outage")

		// The epoch rolled over while we were blind to it.
		epoch.Store(101)
		failing.Store(false)

		freshKey := staleKey
		freshKey.Epoch = 101
		require.Eventually(t, func() bool {
			return len(buf.Read(freshKey)) >= 1
		}, 5*time.Second, 5*time.Millisecond, "expected samples under the fresh epoch after recovery")

		cancel()

		assert.Equal(t, 1, handler.count(slog.LevelInfo, msgEpochRecovered), "recovery should be reported once")
		assert.NotEmpty(t, buf.Read(staleKey), "samples taken during the outage should still be buffered")
	})

	t.Run("refuses to probe when no epoch has ever been fetched, logging once", func(t *testing.T) {
		t.Parallel()

		devicePK, peerPK, linkPK := newPK(26), newPK(27), newPK(28)

		var fetches atomic.Int64
		getCurrentEpoch := func(context.Context) (uint64, error) {
			fetches.Add(1)
			return 0, errors.New("ledger rpc unreachable")
		}

		handler := newRecordingHandler()
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.New(handler), &telemetry.PingerConfig{
			LocalDevicePK: devicePK,
			Peers:         singleTunnelPeer(t, peerPK, linkPK),
			Buffer:        buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetCurrentEpoch: getCurrentEpoch,
		})

		const ticks = 3
		for range ticks {
			pinger.Tick(context.Background())
		}

		assert.Empty(t, buf.FlushWithoutReset(), "nothing to key samples by, so nothing should be recorded")
		assert.Equal(t, int64(ticks*3), fetches.Load(), "each tick should retry the fetch three times")
		assert.Equal(t, 1, handler.count(slog.LevelError, msgEpochUnavailable), "should be logged once, not per tick")
	})

	t.Run("stops probing once the cached epoch is too stale", func(t *testing.T) {
		t.Parallel()

		devicePK, peerPK, linkPK := newPK(29), newPK(30), newPK(31)

		var mu sync.Mutex
		now := time.Now().UTC()
		nowFunc := func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return now
		}
		advance := func(d time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			now = now.Add(d)
		}

		var fetches atomic.Int64
		getCurrentEpoch := func(context.Context) (uint64, error) {
			if fetches.Add(1) == 1 {
				return 100, nil
			}
			return 0, errors.New("ledger rpc unreachable")
		}

		handler := newRecordingHandler()
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.New(handler), &telemetry.PingerConfig{
			LocalDevicePK: devicePK,
			Peers:         singleTunnelPeer(t, peerPK, linkPK),
			Buffer:        buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetCurrentEpoch:   getCurrentEpoch,
			MaxEpochStaleness: time.Hour,
			NowFunc:           nowFunc,
		})

		pinger.Tick(context.Background())

		key := telemetry.PartitionKey{OriginDevicePK: devicePK, TargetDevicePK: peerPK, LinkPK: linkPK, Epoch: 100}
		require.Len(t, buf.Read(key), 1, "expected a sample with the fresh epoch")

		// Past the bound, a rollover is likely enough that these samples would be misattributed to
		// the previous epoch's account.
		advance(2 * time.Hour)
		pinger.Tick(context.Background())
		pinger.Tick(context.Background())

		assert.Len(t, buf.Read(key), 1, "should not have probed with an epoch past the staleness bound")
		assert.Equal(t, 1, handler.count(slog.LevelWarn, msgEpochTooStale), "should be logged once, not per tick")
		assert.Equal(t, int64(1), fetches.Load(), "the probe path should not fetch once an epoch is cached")
	})

	// Regression test for the coalescing that made the outage expensive: a failing fetch burns
	// ~130s across its retries, and the probe ticker only buffers one tick, so an inline fetch
	// silently dropped a dozen probe opportunities per failure.
	t.Run("probe path does not block on the epoch fetch", func(t *testing.T) {
		t.Parallel()

		devicePK, peerPK, linkPK := newPK(32), newPK(33), newPK(34)

		release := make(chan struct{})
		defer close(release)

		var fetches atomic.Int64
		getCurrentEpoch := func(ctx context.Context) (uint64, error) {
			if fetches.Add(1) == 1 {
				return 100, nil
			}
			// Stands in for a blackholed endpoint: no FIN, no RST, no answer.
			select {
			case <-release:
			case <-ctx.Done():
			}
			return 0, errors.New("ledger rpc unreachable")
		}

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.New(newRecordingHandler()), &telemetry.PingerConfig{
			LocalDevicePK:        devicePK,
			Interval:             5 * time.Millisecond,
			EpochRefreshInterval: 5 * time.Millisecond,
			Peers:                singleTunnelPeer(t, peerPK, linkPK),
			Buffer:               buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetCurrentEpoch: getCurrentEpoch,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Seed the cache so the refresh loop's next fetch is the one that hangs.
		pinger.Tick(ctx)
		require.Equal(t, int64(1), fetches.Load())

		go func() { _ = pinger.Run(ctx) }()

		key := telemetry.PartitionKey{OriginDevicePK: devicePK, TargetDevicePK: peerPK, LinkPK: linkPK, Epoch: 100}
		require.Eventually(t, func() bool {
			return len(buf.Read(key)) >= 10
		}, 2*time.Second, 5*time.Millisecond, "probing should keep its cadence while the epoch fetch hangs")

		assert.Equal(t, int64(2), fetches.Load(), "the probe path should not touch the ledger")
	})
}

// singleTunnelPeer builds a peer discovery with one reachable peer over a loopback tunnel.
func singleTunnelPeer(t *testing.T, peerPK, linkPK solana.PublicKey) *mockPeerDiscovery {
	t.Helper()

	peers := newMockPeerDiscovery()
	peers.UpdatePeers(t, []*telemetry.Peer{
		{
			DevicePK: peerPK,
			LinkPK:   linkPK,
			Tunnel: &netutil.LocalTunnel{
				Interface: "tunEpoch",
				SourceIP:  ipv4([4]uint8{127, 0, 0, 1}),
				TargetIP:  ipv4([4]uint8{127, 0, 0, 2}),
			},
		},
	})
	return peers
}

// Log messages the pinger collapses to one line per fresh/stale transition. Asserted on directly
// because "logs once, not per tick" is the behavior under test.
const (
	msgEpochFellBack    = "Failed to get current epoch, probing with the last known epoch"
	msgEpochRecovered   = "Epoch fetch recovered"
	msgEpochUnavailable = "No epoch available and none cached, skipping probes until the ledger answers"
	msgEpochTooStale    = "Cached epoch is too stale to probe with, skipping probes until the ledger answers"
)

// recordingHandler captures log records so tests can assert on log-once behavior.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func newRecordingHandler() *recordingHandler { return &recordingHandler{} }

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *recordingHandler) WithGroup(string) slog.Handler { return h }

func (h *recordingHandler) messages(level slog.Level) []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	var out []string
	for _, r := range h.records {
		if r.Level == level {
			out = append(out, r.Message)
		}
	}
	return out
}

func (h *recordingHandler) count(level slog.Level, message string) int {
	n := 0
	for _, m := range h.messages(level) {
		if m == message {
			n++
		}
	}
	return n
}

type mockSender struct {
	rtt time.Duration
	err error
}

func (m *mockSender) Probe(context.Context) (time.Duration, error) {
	return m.rtt, m.err
}

func (m *mockSender) Close() error { return nil }

func (m *mockSender) LocalAddr() *net.UDPAddr { return nil }
