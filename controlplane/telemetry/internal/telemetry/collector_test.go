package telemetry_test

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_Collector(t *testing.T) {

	t.Run("single collector no peers", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		reflector := newTestReflector(t)
		devicePubKey := stringToPubkey("device")
		telemetryProgram := newMockTelemetryProgramClient()
		collector := newTestCollector(t, log, devicePubKey.String(), reflector, map[string]*telemetry.Peer{}, telemetryProgram, 250*time.Millisecond)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			require.NoError(t, collector.Run(ctx))
		}()

		require.Never(t, func() bool {
			return len(telemetryProgram.GetSamples(t)) > 0
		}, 2*time.Second, 100*time.Millisecond)
	})

	t.Run("shutdown cleanly", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		collector := newTestCollector(t, log, stringToPubkey("device").String(), newTestReflector(t), map[string]*telemetry.Peer{}, newMockTelemetryProgramClient(), 250*time.Millisecond)
		ctx, cancel := context.WithCancel(t.Context())

		done := make(chan struct{})
		go func() {
			require.NoError(t, collector.Run(ctx))
			close(done)
		}()

		cancel()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("collector did not shut down in time")
		}
	})

	t.Run("multiple collectors", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		reflector1 := newTestReflector(t)
		reflector2 := newTestReflector(t)

		// We have 2 devices with real reflectors and 1 device who's probes will be lost.
		device1PubKey := stringToPubkey("device1")
		device2PubKey := stringToPubkey("device2")
		device3PubKey := stringToPubkey("device3")

		telemetryProgram1 := newMockTelemetryProgramClient()
		collector1 := newTestCollector(t, log.With("runtime", "collector1"), device1PubKey.String(), reflector1, map[string]*telemetry.Peer{
			"link1-2": {
				LinkPubkey:   "link1-2",
				DevicePubkey: device2PubKey.String(),
				DeviceAddr:   reflector2.LocalAddr().(*net.UDPAddr),
			},
			"link1-3": {
				LinkPubkey:   "link1-3",
				DevicePubkey: device3PubKey.String(),
				// This target should not be reachable.
				DeviceAddr: &net.UDPAddr{IP: net.IPv4(10, 241, 1, 3), Port: 1862},
			},
		}, telemetryProgram1, 250*time.Millisecond)

		telemetryProgram2 := newMockTelemetryProgramClient()
		collector2 := newTestCollector(t, log.With("runtime", "collector2"), device2PubKey.String(), reflector2, map[string]*telemetry.Peer{
			"link2-1": {
				LinkPubkey:   "link2-1",
				DevicePubkey: device1PubKey.String(),
				DeviceAddr:   reflector1.LocalAddr().(*net.UDPAddr),
			},
			"link2-3": {
				LinkPubkey:   "link2-3",
				DevicePubkey: device3PubKey.String(),
				// This target should not be reachable.
				DeviceAddr: &net.UDPAddr{IP: net.IPv4(10, 241, 1, 3), Port: 1862},
			},
		}, telemetryProgram2, 250*time.Millisecond)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			require.NoError(t, collector1.Run(ctx))
		}()
		go func() {
			require.NoError(t, collector2.Run(ctx))
		}()

		require.Eventually(t, func() bool {
			return len(telemetryProgram1.GetSamples(t)) >= 4 && len(telemetryProgram2.GetSamples(t)) >= 4
		}, 5*time.Second, 100*time.Millisecond)

		samples1 := telemetryProgram1.GetSamples(t)
		samplesByPeer1 := map[string][]telemetry.Sample{}
		for _, sample := range samples1 {
			samplesByPeer1[sample.PeerKey()] = append(samplesByPeer1[sample.PeerKey()], sample)
		}
		require.Len(t, samplesByPeer1, 2)
		require.GreaterOrEqual(t, len(samplesByPeer1["link1-2"]), 2)
		require.GreaterOrEqual(t, len(samplesByPeer1["link1-3"]), 2)

		seen := map[string]bool{}
		for _, sample := range samples1 {
			require.False(t, seen[sample.Key()], "sample %+v should not have been seen before: %+v", sample, samples1)
			seen[sample.Key()] = true

			switch sample.Device {
			case device1PubKey.String():
				t.Fatal("device 1 should not have samples for itself")
			case device2PubKey.String():
				require.Greater(t, sample.RTT, time.Duration(0), "device2 should have RTT>0, but got in %d %+v", sample.RTT, sample)
				require.False(t, sample.Loss, "device2 should have loss=false, but got in %t %+v", sample.Loss, sample)
			case device3PubKey.String():
				require.Equal(t, sample.RTT, time.Duration(0), "device3 should have RTT=0, but got in %d %+v", sample.RTT, sample)
				require.True(t, sample.Loss, "device3 should have loss=true, but got in %t %+v", sample.Loss, sample)
			}
		}

		samples2 := telemetryProgram2.GetSamples(t)
		samplesByPeer2 := map[string][]telemetry.Sample{}
		for _, sample := range samples2 {
			samplesByPeer2[sample.PeerKey()] = append(samplesByPeer2[sample.PeerKey()], sample)
		}
		require.Len(t, samplesByPeer2, 2)
		require.GreaterOrEqual(t, len(samplesByPeer2["link2-1"]), 2)
		require.GreaterOrEqual(t, len(samplesByPeer2["link2-3"]), 2)

		seen = map[string]bool{}
		for _, sample := range samples2 {
			require.False(t, seen[sample.Key()], "sample %+v should not have been seen before: %+v", sample, samples2)
			seen[sample.Key()] = true

			switch sample.Device {
			case device1PubKey.String():
				require.Greater(t, sample.RTT, time.Duration(0), "device1 should have RTT>0, but got in %d %+v", sample.RTT, sample)
				require.False(t, sample.Loss, "device1 should have loss=false, but got in %t %+v", sample.Loss, sample)
			case device2PubKey.String():
				t.Fatal("device 2 should not have samples for itself")
			case device3PubKey.String():
				require.Equal(t, sample.RTT, time.Duration(0), "device3 should have RTT=0, but got in %d %+v", sample.RTT, sample)
				require.True(t, sample.Loss, "device3 should have loss=true, but got in %t %+v", sample.Loss, sample)
			}
		}
	})

	t.Run("collector does nothing if no peers are available", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		reflector := newTestReflector(t)
		mockProgram := newMockTelemetryProgramClient()
		mockDiscovery := newMockPeerDiscovery()

		// Empty peer set, and we never update it
		mockDiscovery.UpdatePeers(t, map[string]*telemetry.Peer{})

		collector := newTestCollector(t, log, "device-x", reflector, map[string]*telemetry.Peer{}, mockProgram, 250*time.Millisecond)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			require.NoError(t, collector.Run(ctx))
		}()

		require.Never(t, func() bool {
			return len(mockProgram.GetSamples(t)) > 0
		}, 2*time.Second, 100*time.Millisecond)
	})
}

func newTestReflector(t *testing.T) *twamplight.Reflector {
	reflector, err := twamplight.NewReflector(log, 0, 1*time.Second)
	require.NoError(t, err)

	t.Cleanup(func() {
		reflector.Close()
	})

	return reflector
}

func newTestCollector(t *testing.T, log *slog.Logger, localDevicePubKey string, reflector *twamplight.Reflector, peers map[string]*telemetry.Peer, telemetryProgramClient telemetry.TelemetryProgramClient, submissionInterval time.Duration) *telemetry.Collector {
	peerDiscovery := newMockPeerDiscovery()
	peerDiscovery.UpdatePeers(t, peers)

	collector, err := telemetry.New(log, telemetry.Config{
		LocalDevicePubkey:      localDevicePubKey,
		ProbeInterval:          100 * time.Millisecond,
		SubmissionInterval:     submissionInterval,
		TWAMPSenderTimeout:     1 * time.Second,
		TWAMPReflector:         reflector,
		PeerDiscovery:          peerDiscovery,
		TelemetryProgramClient: telemetryProgramClient,
	})
	require.NoError(t, err)

	return collector
}
