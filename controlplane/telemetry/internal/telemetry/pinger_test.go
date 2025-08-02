package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
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

		buffer := buffer.NewPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
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

		buffer := buffer.NewPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
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

		buffer := buffer.NewPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
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
		buffer := buffer.NewPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
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
		buffer := buffer.NewPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
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

		buffer := buffer.NewPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
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
