package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_Pinger(t *testing.T) {
	t.Parallel()

	t.Run("adds_successful_sample", func(t *testing.T) {
		t.Parallel()

		mockPeers := newMockPeerDiscovery()
		mockPeers.UpdatePeers(t, map[string]*telemetry.Peer{
			"peer1": {
				DevicePubkey: "device1",
				LinkPubkey:   "link1",
				DeviceAddr:   &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345},
			},
		})

		sampleBuf := telemetry.NewSampleBuffer(10)

		mockSender := &mockSender{rtt: 42 * time.Millisecond, calls: make(chan struct{}, 1)}
		getSender := func(string, *telemetry.Peer) twamplight.Sender { return mockSender }

		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			Peers:     mockPeers,
			Buffer:    sampleBuf,
			GetSender: getSender,
		})

		pinger.Tick(context.Background())

		samples := sampleBuf.Read()
		require.Len(t, samples, 1)
		assert.False(t, samples[0].Loss)
		assert.Equal(t, "device1", samples[0].Device)
		assert.Equal(t, "link1", samples[0].Link)
		assert.Greater(t, samples[0].RTT, time.Duration(0))
	})

	t.Run("records loss when GetSender returns nil", func(t *testing.T) {
		t.Parallel()

		peers := newMockPeerDiscovery()
		buffer := telemetry.NewSampleBuffer(10)

		peers.UpdatePeers(t, map[string]*telemetry.Peer{
			"peer1": {
				DeviceAddr:   &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345},
				DevicePubkey: "device-1",
				LinkPubkey:   "link-1",
			},
		})

		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			Peers:     peers,
			Buffer:    buffer,
			GetSender: func(string, *telemetry.Peer) twamplight.Sender { return nil },
		})

		pinger.Tick(context.Background())

		samples := buffer.Read()
		require.Len(t, samples, 1)
		assert.True(t, samples[0].Loss)
		assert.Equal(t, "device-1", samples[0].Device)
		assert.Equal(t, "link-1", samples[0].Link)
		assert.Zero(t, samples[0].RTT)
	})

	t.Run("records RTT when sender succeeds", func(t *testing.T) {
		t.Parallel()

		peers := newMockPeerDiscovery()
		buffer := telemetry.NewSampleBuffer(10)

		peers.UpdatePeers(t, map[string]*telemetry.Peer{
			"peer2": {
				DeviceAddr:   &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9998},
				DevicePubkey: "device-2",
				LinkPubkey:   "link-2",
			},
		})

		mockSender := &mockSender{rtt: 42 * time.Millisecond, calls: make(chan struct{}, 1)}
		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			Peers:     peers,
			Buffer:    buffer,
			GetSender: func(string, *telemetry.Peer) twamplight.Sender { return mockSender },
		})

		pinger.Tick(context.Background())

		samples := buffer.Read()
		require.Len(t, samples, 1)
		assert.False(t, samples[0].Loss)
		assert.Equal(t, "device-2", samples[0].Device)
		assert.Equal(t, "link-2", samples[0].Link)
		assert.Equal(t, 42*time.Millisecond, samples[0].RTT)
	})

	t.Run("records loss when sender returns error", func(t *testing.T) {
		t.Parallel()

		peers := newMockPeerDiscovery()
		buffer := telemetry.NewSampleBuffer(10)

		peers.UpdatePeers(t, map[string]*telemetry.Peer{
			"peer3": {
				DeviceAddr:   &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9997},
				DevicePubkey: "device-3",
				LinkPubkey:   "link-3",
			},
		})

		mockSender := &mockSender{
			rtt:   0,
			err:   errors.New("network failure"),
			calls: make(chan struct{}, 1),
		}

		pinger := telemetry.NewPinger(slog.Default(), &telemetry.PingerConfig{
			Peers:     peers,
			Buffer:    buffer,
			GetSender: func(string, *telemetry.Peer) twamplight.Sender { return mockSender },
		})

		pinger.Tick(context.Background())

		samples := buffer.Read()
		require.Len(t, samples, 1)
		assert.True(t, samples[0].Loss)
		assert.Equal(t, "device-3", samples[0].Device)
		assert.Equal(t, "link-3", samples[0].Link)
		assert.Zero(t, samples[0].RTT)
	})
}

type mockSender struct {
	rtt   time.Duration
	err   error
	calls chan struct{}
}

func (m *mockSender) Probe(ctx context.Context) (time.Duration, error) {
	select {
	case m.calls <- struct{}{}:
	default:
	}
	return m.rtt, m.err
}

func (m *mockSender) Close() error { return nil }
