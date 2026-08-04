package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/metrics"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
			GetEpochInfo:  staticEpoch(epoch),
		})

		pinger.RefreshEpoch(context.Background())
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
			GetEpochInfo:  staticEpoch(100),
		})

		pinger.RefreshEpoch(context.Background())
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
			GetEpochInfo:  staticEpoch(100),
		})

		pinger.RefreshEpoch(context.Background())
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
			GetEpochInfo:  staticEpoch(100),
		})

		pinger.RefreshEpoch(context.Background())
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

	t.Run("retries the epoch fetch before succeeding", func(t *testing.T) {
		t.Parallel()

		devicePK := newPK(10)
		peerPK := newPK(11)
		linkPK := newPK(12)

		attempts := 0
		getEpochInfo := func(ctx context.Context) (telemetry.EpochInfo, error) {
			attempts++
			if attempts < 3 {
				return telemetry.EpochInfo{}, errors.New("transient failure")
			}
			return telemetry.EpochInfo{Epoch: 123}, nil
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
			LocalDevicePK: devicePK,
			Peers:         mockPeers,
			Buffer:        buffer,
			GetSender:     func(context.Context, *telemetry.Peer) twamplight.Sender { return mockSender },
			GetEpochInfo:  getEpochInfo,
		})

		pinger.RefreshEpoch(context.Background())
		pinger.Tick(context.Background())

		assert.Equal(t, 3, attempts, "expected exactly 3 attempts at GetEpochInfo")

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

	t.Run("gives up when the epoch fetch exceeds max retries", func(t *testing.T) {
		t.Parallel()

		devicePK := newPK(13)

		var attempts int
		getEpochInfo := func(ctx context.Context) (telemetry.EpochInfo, error) {
			attempts++
			return telemetry.EpochInfo{}, errors.New("persistent failure")
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
			LocalDevicePK: devicePK,
			Peers:         mockPeers,
			Buffer:        buffer,
			GetSender:     func(context.Context, *telemetry.Peer) twamplight.Sender { return nil },
			GetEpochInfo:  getEpochInfo,
		})

		pinger.RefreshEpoch(context.Background())
		pinger.Tick(context.Background())

		assert.Equal(t, 3, attempts, "should have retried GetEpochInfo exactly 3 times")

		samples := buffer.FlushWithoutReset()
		assert.Empty(t, samples, "should not record any samples if epoch retrieval fails")
	})

	// The epoch is only used to build the partition key, so a ledger RPC outage should cost epoch
	// precision, not measurements. See #4125.
	t.Run("probes with the last known epoch while the epoch fetch fails", func(t *testing.T) {
		t.Parallel()

		devicePK, peerPK, linkPK := newPK(20), newPK(21), newPK(22)

		var fetches atomic.Int64
		getEpochInfo := func(context.Context) (telemetry.EpochInfo, error) {
			if fetches.Add(1) == 1 {
				return telemetry.EpochInfo{Epoch: 100}, nil
			}
			return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
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
			GetEpochInfo: getEpochInfo,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Seed the cache with the only successful fetch, then let the refresh loop fail from here on.
		pinger.RefreshEpoch(ctx)
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
		getEpochInfo := func(context.Context) (telemetry.EpochInfo, error) {
			if failing.Load() {
				return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
			}
			return telemetry.EpochInfo{Epoch: epoch.Load()}, nil
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
			GetEpochInfo: getEpochInfo,
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
		getEpochInfo := func(context.Context) (telemetry.EpochInfo, error) {
			fetches.Add(1)
			return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
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
			GetEpochInfo: getEpochInfo,
		})

		pinger.RefreshEpoch(context.Background())

		const ticks = 3
		for range ticks {
			pinger.Tick(context.Background())
		}

		assert.Empty(t, buf.FlushWithoutReset(), "nothing to key samples by, so nothing should be recorded")
		assert.Equal(t, int64(3), fetches.Load(), "only the refresh should touch the ledger, retrying three times")
		assert.Equal(t, 1, handler.count(slog.LevelError, msgEpochUnavailable), "should be logged once, not per tick")
	})

	t.Run("stops probing once the cached epoch is too stale", func(t *testing.T) {
		t.Parallel()

		devicePK, peerPK, linkPK := newPK(29), newPK(30), newPK(31)

		clock := newFakeClock()

		var fetches atomic.Int64
		getEpochInfo := func(context.Context) (telemetry.EpochInfo, error) {
			if fetches.Add(1) == 1 {
				return telemetry.EpochInfo{Epoch: 100}, nil
			}
			return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
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
			GetEpochInfo:      getEpochInfo,
			MaxEpochStaleness: time.Hour,
			NowFunc:           clock.now,
		})

		pinger.RefreshEpoch(context.Background())
		pinger.Tick(context.Background())

		key := telemetry.PartitionKey{OriginDevicePK: devicePK, TargetDevicePK: peerPK, LinkPK: linkPK, Epoch: 100}
		require.Len(t, buf.Read(key), 1, "expected a sample with the fresh epoch")

		// Past the bound the sample buffer can no longer hold the backlog, and the submitter drops all
		// of it at once rather than the oldest slice.
		clock.advance(2 * time.Hour)
		pinger.Tick(context.Background())
		pinger.Tick(context.Background())

		assert.Len(t, buf.Read(key), 1, "should not have probed with an epoch past the staleness bound")
		assert.Equal(t, 1, handler.count(slog.LevelWarn, msgEpochTooStale), "should be logged once, not per tick")
		assert.Equal(t, int64(1), fetches.Load(), "the probe path should not fetch once an epoch is cached")
	})

	// A cached epoch that has rolled over is worse than a stale one: the samples land in the previous
	// epoch's account with timestamps inside the next epoch's window, where a time-range query scoped
	// to the new epoch never looks.
	t.Run("stops probing once the cached epoch has likely rolled over", func(t *testing.T) {
		t.Parallel()

		devicePK, peerPK, linkPK := newPK(35), newPK(36), newPK(37)

		clock := newFakeClock()

		// 10 slots left in the epoch. Without a measured slot rate the projection uses 340ms per slot,
		// so the epoch ends 3.4s from the fetch.
		getEpochInfo := func(context.Context) (telemetry.EpochInfo, error) {
			return telemetry.EpochInfo{Epoch: 100, SlotIndex: 431_990, SlotsInEpoch: 432_000}, nil
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
			GetEpochInfo: getEpochInfo,
			// Deliberately far larger than the projected epoch end, so only the rollover bound can be
			// what stops the probing below.
			MaxEpochStaleness: 24 * time.Hour,
			NowFunc:           clock.now,
		})

		pinger.RefreshEpoch(context.Background())
		pinger.Tick(context.Background())

		key := telemetry.PartitionKey{OriginDevicePK: devicePK, TargetDevicePK: peerPK, LinkPK: linkPK, Epoch: 100}
		require.Len(t, buf.Read(key), 1, "expected a sample inside the epoch")

		clock.advance(2 * time.Second)
		pinger.Tick(context.Background())
		require.Len(t, buf.Read(key), 2, "still inside the epoch, so probing should continue")

		clock.advance(2 * time.Second)
		pinger.Tick(context.Background())
		pinger.Tick(context.Background())

		assert.Len(t, buf.Read(key), 2, "should not have probed past the projected rollover")
		assert.Equal(t, 1, handler.count(slog.LevelWarn, msgEpochEnded), "should be logged once, not per tick")
		assert.Zero(t, handler.count(slog.LevelWarn, msgEpochTooStale), "the staleness bound is not what stopped it")
	})

	// The 400ms slot target is not the DoubleZero ledger's real slot time — a 432k-slot epoch lands in
	// ~44h, i.e. ~367ms — so projecting the epoch's end from the constant overshoots the real boundary.
	// The measured rate comes free from the AbsoluteSlot readings the refresh loop already makes.
	t.Run("projects the epoch end from the measured slot rate", func(t *testing.T) {
		t.Parallel()

		devicePK, peerPK, linkPK := newPK(38), newPK(39), newPK(40)

		clock := newFakeClock()

		var info atomic.Pointer[telemetry.EpochInfo]
		info.Store(&telemetry.EpochInfo{Epoch: 100, SlotIndex: 0, SlotsInEpoch: 10_000, AbsoluteSlot: 1_000_000})

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.New(newRecordingHandler()), &telemetry.PingerConfig{
			LocalDevicePK: devicePK,
			Peers:         singleTunnelPeer(t, peerPK, linkPK),
			Buffer:        buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetEpochInfo:      func(context.Context) (telemetry.EpochInfo, error) { return *info.Load(), nil },
			MaxEpochStaleness: 24 * time.Hour,
			NowFunc:           clock.now,
		})

		// First reading only establishes the baseline.
		pinger.RefreshEpoch(context.Background())

		// 1200 slots in 600s is 500ms per slot, well past the 5m baseline the estimator needs. With
		// 8800 slots left the epoch ends 8800*500ms*0.95 = 4180s out; the 340ms fallback would have
		// put it at 2992s.
		clock.advance(10 * time.Minute)
		info.Store(&telemetry.EpochInfo{Epoch: 100, SlotIndex: 1_200, SlotsInEpoch: 10_000, AbsoluteSlot: 1_001_200})
		pinger.RefreshEpoch(context.Background())

		key := telemetry.PartitionKey{OriginDevicePK: devicePK, TargetDevicePK: peerPK, LinkPK: linkPK, Epoch: 100}

		clock.advance(3200 * time.Second)
		pinger.Tick(context.Background())
		require.Len(t, buf.Read(key), 1, "the fallback slot time would have called the epoch over by now")

		clock.advance(1200 * time.Second)
		pinger.Tick(context.Background())
		assert.Len(t, buf.Read(key), 1, "should not have probed past the measured epoch end")
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
		getEpochInfo := func(ctx context.Context) (telemetry.EpochInfo, error) {
			if fetches.Add(1) == 1 {
				return telemetry.EpochInfo{Epoch: 100}, nil
			}
			// Stands in for a blackholed endpoint: no FIN, no RST, no answer.
			select {
			case <-release:
			case <-ctx.Done():
			}
			return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
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
			GetEpochInfo: getEpochInfo,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Seed the cache so the refresh loop's next fetch is the one that hangs.
		pinger.RefreshEpoch(ctx)
		require.Equal(t, int64(1), fetches.Load())

		go func() { _ = pinger.Run(ctx) }()

		key := telemetry.PartitionKey{OriginDevicePK: devicePK, TargetDevicePK: peerPK, LinkPK: linkPK, Epoch: 100}
		require.Eventually(t, func() bool {
			return len(buf.Read(key)) >= 10
		}, 2*time.Second, 5*time.Millisecond, "probing should keep its cadence while the epoch fetch hangs")

		assert.Equal(t, int64(2), fetches.Load(), "the probe path should not touch the ledger")
	})

	// A partially degraded endpoint flaps rather than dying, and the fallback is only worth having if
	// it stays quiet through that.
	t.Run("does not report a fallback the retries recovered from", func(t *testing.T) {
		t.Parallel()

		devicePK, peerPK, linkPK := newPK(41), newPK(42), newPK(43)

		var failing atomic.Bool
		getEpochInfo := func(context.Context) (telemetry.EpochInfo, error) {
			if failing.Load() {
				return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
			}
			return telemetry.EpochInfo{Epoch: 100}, nil
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
			GetEpochInfo: getEpochInfo,
		})

		ctx := context.Background()
		pinger.RefreshEpoch(ctx)

		// Two flaps: each one fails a whole fetch, but neither reaches the consecutive-failure
		// threshold, so neither is worth an operator's attention.
		for range 2 {
			failing.Store(true)
			pinger.RefreshEpoch(ctx)
			failing.Store(false)
			pinger.RefreshEpoch(ctx)
		}

		assert.Empty(t, handler.messages(slog.LevelWarn), "a flap the retries rode out is not a fallback")
		assert.Empty(t, handler.messages(slog.LevelInfo), "and there is nothing to report recovering from")

		// A sustained outage still gets exactly one Warn, on the third consecutive failure.
		failing.Store(true)
		pinger.RefreshEpoch(ctx)
		pinger.RefreshEpoch(ctx)
		assert.Zero(t, handler.count(slog.LevelWarn, msgEpochFellBack), "two failures is still a flap")

		pinger.RefreshEpoch(ctx)
		assert.Equal(t, 1, handler.count(slog.LevelWarn, msgEpochFellBack), "the third should report the fallback")

		pinger.RefreshEpoch(ctx)
		pinger.RefreshEpoch(ctx)
		assert.Equal(t, 1, handler.count(slog.LevelWarn, msgEpochFellBack), "and only once for the whole outage")

		failing.Store(false)
		pinger.RefreshEpoch(ctx)
		assert.Equal(t, 1, handler.count(slog.LevelInfo, msgEpochRecovered), "the reported fallback gets a resolution")
	})
}

// The epoch cache metrics are package-level globals, so these subtests cannot run in parallel with
// each other or with TestAgentTelemetry_Pinger's. A sequential top-level test never overlaps a
// parallel one.
func TestAgentTelemetry_PingerEpochMetrics(t *testing.T) {
	newPK := func(b byte) solana.PublicKey {
		var pk solana.PublicKey
		pk[0] = b
		return pk
	}

	t.Run("stale age gauge tracks the cached epoch's age and clears on recovery", func(t *testing.T) {
		clock := newFakeClock()

		var failing atomic.Bool
		getEpochInfo := func(context.Context) (telemetry.EpochInfo, error) {
			if failing.Load() {
				return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
			}
			return telemetry.EpochInfo{Epoch: 100}, nil
		}

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.New(newRecordingHandler()), &telemetry.PingerConfig{
			LocalDevicePK: newPK(50),
			Peers:         singleTunnelPeer(t, newPK(51), newPK(52)),
			Buffer:        buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetEpochInfo:      getEpochInfo,
			MaxEpochStaleness: 24 * time.Hour,
			NowFunc:           clock.now,
		})

		ctx := context.Background()
		pinger.RefreshEpoch(ctx)
		require.Zero(t, testutil.ToFloat64(metrics.EpochCacheStaleAge), "a fresh cache is not stale")

		failing.Store(true)
		clock.advance(90 * time.Minute)
		pinger.RefreshEpoch(ctx)
		assert.Equal(t, (90 * time.Minute).Seconds(), testutil.ToFloat64(metrics.EpochCacheStaleAge))

		clock.advance(30 * time.Minute)
		pinger.RefreshEpoch(ctx)
		assert.Equal(t, (2 * time.Hour).Seconds(), testutil.ToFloat64(metrics.EpochCacheStaleAge))

		failing.Store(false)
		pinger.RefreshEpoch(ctx)
		assert.Zero(t, testutil.ToFloat64(metrics.EpochCacheStaleAge), "recovery should clear the gauge")
	})

	// Restarted mid-outage with nothing cached is the worst state there is — no probing at all — and
	// it used to read exactly like a healthy agent.
	t.Run("stale age gauge reports a sentinel when no epoch has ever been fetched", func(t *testing.T) {
		metrics.EpochCacheStaleAge.Set(0)

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.New(newRecordingHandler()), &telemetry.PingerConfig{
			LocalDevicePK: newPK(53),
			Peers:         singleTunnelPeer(t, newPK(54), newPK(55)),
			Buffer:        buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetEpochInfo: func(context.Context) (telemetry.EpochInfo, error) {
				return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
			},
		})

		pinger.RefreshEpoch(context.Background())

		assert.True(t, math.IsInf(testutil.ToFloat64(metrics.EpochCacheStaleAge), 1),
			"nothing cached should not read the same as a fresh cache")
	})

	t.Run("each refusal and each failed attempt is counted separately", func(t *testing.T) {
		before := map[string]float64{}
		for _, errorType := range []string{
			metrics.ErrorTypePingerEpochNeverFetched,
			metrics.ErrorTypePingerEpochTooStale,
			metrics.ErrorTypePingerEpochEnded,
			metrics.ErrorTypePingerEpochFetchFailed,
		} {
			before[errorType] = testutil.ToFloat64(metrics.Errors.WithLabelValues(errorType))
		}
		delta := func(errorType string) float64 {
			return testutil.ToFloat64(metrics.Errors.WithLabelValues(errorType)) - before[errorType]
		}

		clock := newFakeClock()

		var failing atomic.Bool
		failing.Store(true)
		var info atomic.Pointer[telemetry.EpochInfo]
		info.Store(&telemetry.EpochInfo{Epoch: 100})

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.New(newRecordingHandler()), &telemetry.PingerConfig{
			LocalDevicePK: newPK(56),
			Peers:         singleTunnelPeer(t, newPK(57), newPK(58)),
			Buffer:        buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetEpochInfo: func(context.Context) (telemetry.EpochInfo, error) {
				if failing.Load() {
					return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
				}
				return *info.Load(), nil
			},
			MaxEpochStaleness: time.Hour,
			NowFunc:           clock.now,
		})

		ctx := context.Background()

		// Nothing has ever been fetched: three failed attempts, one refusal.
		pinger.RefreshEpoch(ctx)
		pinger.Tick(ctx)
		assert.Equal(t, 3.0, delta(metrics.ErrorTypePingerEpochFetchFailed), "one per attempt, not one per fetch")
		assert.Equal(t, 1.0, delta(metrics.ErrorTypePingerEpochNeverFetched))
		assert.Zero(t, delta(metrics.ErrorTypePingerEpochTooStale), "a missing epoch is not a stale one")

		// Cached, then left to go past the staleness bound.
		failing.Store(false)
		pinger.RefreshEpoch(ctx)
		clock.advance(2 * time.Hour)
		pinger.Tick(ctx)
		assert.Equal(t, 1.0, delta(metrics.ErrorTypePingerEpochTooStale))
		assert.Equal(t, 1.0, delta(metrics.ErrorTypePingerEpochNeverFetched), "still just the one")

		// Cached with a slot position that puts the rollover 3.4s out, then left past it.
		info.Store(&telemetry.EpochInfo{Epoch: 101, SlotIndex: 431_990, SlotsInEpoch: 432_000})
		pinger.RefreshEpoch(ctx)
		clock.advance(10 * time.Second)
		pinger.Tick(ctx)
		assert.Equal(t, 1.0, delta(metrics.ErrorTypePingerEpochEnded))
		assert.Equal(t, 1.0, delta(metrics.ErrorTypePingerEpochTooStale), "a rollover is not staleness either")
	})

	// ErrorTypePingerEpochFetchFailed above fires on every retry; this one fires once per exhausted
	// batch, which is what the refresh loop actually reacts to and the only signal for an endpoint
	// that fails intermittently without ever going stale enough to stop probing.
	t.Run("counts every exhausted epoch fetch, not every attempt", func(t *testing.T) {
		before := testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochFetch))

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		pinger := telemetry.NewPinger(slog.New(newRecordingHandler()), &telemetry.PingerConfig{
			LocalDevicePK: newPK(62),
			Peers:         singleTunnelPeer(t, newPK(63), newPK(64)),
			Buffer:        buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetEpochInfo: func(context.Context) (telemetry.EpochInfo, error) {
				return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
			},
		})

		ctx := context.Background()
		for range 5 {
			pinger.RefreshEpoch(ctx)
		}
		assert.Equal(t, 5.0, testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochFetch))-before,
			"every exhausted fetch should be counted once, not once per retry")

		// Shutdown is not a fetch failure.
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		before = testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochFetch))
		pinger.RefreshEpoch(cancelledCtx)
		assert.Zero(t, testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochFetch))-before,
			"a cancelled context should not be counted as a fetch failure")
	})

	// The inline fetch in the probe path made Tick a second writer of the epoch cache, so a tick could
	// act on a read the refresh loop had already replaced: an ERROR and an error-counter increment on
	// an agent with a perfectly good epoch cached.
	t.Run("a flapping fetch never reports an error while an epoch is cached", func(t *testing.T) {
		before := testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochNeverFetched))

		var flapping atomic.Bool
		flapping.Store(true)
		var fetches atomic.Int64
		getEpochInfo := func(context.Context) (telemetry.EpochInfo, error) {
			if flapping.Load() && fetches.Add(1)%2 == 0 {
				return telemetry.EpochInfo{}, errors.New("ledger rpc unreachable")
			}
			return telemetry.EpochInfo{Epoch: 100}, nil
		}

		handler := newRecordingHandler()
		devicePK, peerPK, linkPK := newPK(59), newPK(60), newPK(61)
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](4096)
		pinger := telemetry.NewPinger(slog.New(handler), &telemetry.PingerConfig{
			LocalDevicePK:        devicePK,
			Interval:             time.Millisecond,
			EpochRefreshInterval: time.Millisecond,
			Peers:                singleTunnelPeer(t, peerPK, linkPK),
			Buffer:               buf,
			GetSender: func(context.Context, *telemetry.Peer) twamplight.Sender {
				return &mockSender{rtt: 7 * time.Millisecond}
			},
			GetEpochInfo: getEpochInfo,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Seed the cache first, so every refusal from here on would be a false one.
		pinger.RefreshEpoch(ctx)

		go func() { _ = pinger.Run(ctx) }()

		// Drive Tick directly as well, so the probe path and the refresh loop overlap on the cache.
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				pinger.Tick(ctx)
			}
		}()

		key := telemetry.PartitionKey{OriginDevicePK: devicePK, TargetDevicePK: peerPK, LinkPK: linkPK, Epoch: 100}
		require.Eventually(t, func() bool {
			return len(buf.Read(key)) >= 50 && fetches.Load() >= 10
		}, 10*time.Second, time.Millisecond, "probing and refreshing should both keep running")

		// Let the flapping settle on a success, then check the gauge reflects it.
		flapping.Store(false)
		pinger.RefreshEpoch(ctx)

		cancel()
		wg.Wait()

		assert.Empty(t, handler.messages(slog.LevelError), "no tick should refuse to probe with an epoch cached")
		assert.Equal(t, before, testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochNeverFetched)),
			"and none should be counted as unavailable")
		assert.Zero(t, testutil.ToFloat64(metrics.EpochCacheStaleAge), "the last fetch succeeded, so nothing is stale")
	})
}

// staticEpoch is an epoch fetch that always succeeds with the given epoch and no slot position, so
// only MaxEpochStaleness bounds the cache.
func staticEpoch(epoch uint64) func(context.Context) (telemetry.EpochInfo, error) {
	return func(context.Context) (telemetry.EpochInfo, error) {
		return telemetry.EpochInfo{Epoch: epoch}, nil
	}
}

// fakeClock is an injectable NowFunc for the staleness and rollover bounds. Its times carry no
// monotonic reading, which time.Time.Sub handles by falling back to wall-clock arithmetic.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
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

// Log messages the pinger collapses to one line per transition. Asserted on directly because
// "logs once, not per tick" is the behavior under test.
const (
	msgEpochFellBack    = "Failed to get current epoch, probing with the last known epoch"
	msgEpochRecovered   = "Epoch fetch recovered"
	msgEpochUnavailable = "No epoch available and none cached, skipping probes until the ledger answers"
	msgEpochTooStale    = "Cached epoch is too stale to probe with, skipping probes until the ledger answers"
	msgEpochEnded       = "Cached epoch has likely rolled over, skipping probes until the ledger answers"
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

// reset discards all recorded records, so a test can assert on log-once behavior across
// multiple phases without a fresh handler for each one.
func (h *recordingHandler) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = nil
}

// attrs returns the attributes of the last record at level with the given message, and whether
// one was found.
func (h *recordingHandler) attrs(level slog.Level, message string) (map[string]any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := len(h.records) - 1; i >= 0; i-- {
		r := h.records[i]
		if r.Level != level || r.Message != message {
			continue
		}
		out := make(map[string]any, r.NumAttrs())
		r.Attrs(func(a slog.Attr) bool {
			out[a.Key] = a.Value.Any()
			return true
		})
		return out, true
	}
	return nil, false
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
