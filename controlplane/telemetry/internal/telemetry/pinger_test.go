package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
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

		now := time.Now().UTC()
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

		buffer := telemetry.NewAccountsBuffer()
		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			LocalDevicePK: devicePK,
			Peers:         mockPeers,
			Buffer:        buffer,
			GetSender:     getSender,
		})

		pinger.Tick(context.Background())

		samples := buffer.FlushWithoutReset()
		key := telemetry.AccountKey{
			OriginDevicePK: devicePK,
			TargetDevicePK: peerPK,
			LinkPK:         linkPK,
			Epoch:          telemetry.DeriveEpoch(now),
		}

		s, ok := samples[key]
		require.True(t, ok, "expected sample under account key")
		require.Len(t, s, 1)
		assert.False(t, s[0].Loss)
		assert.Equal(t, 42*time.Millisecond, s[0].RTT)
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

		buffer := telemetry.NewAccountsBuffer()
		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			LocalDevicePK: devicePK,
			Peers:         mockPeers,
			Buffer:        buffer,
			GetSender:     func(_ context.Context, _ *telemetry.Peer) twamplight.Sender { return nil },
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
		buffer := telemetry.NewAccountsBuffer()
		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			LocalDevicePK: devicePK,
			Peers:         mockPeers,
			Buffer:        buffer,
			GetSender:     func(_ context.Context, _ *telemetry.Peer) twamplight.Sender { return mockSender },
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
}

type mockSender struct {
	rtt time.Duration
	err error
}

func (m *mockSender) Probe(context.Context) (time.Duration, error) {
	return m.rtt, m.err
}

func (m *mockSender) Close() error { return nil }
