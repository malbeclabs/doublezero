package telemetry_test

import (
	"context"
	"log/slog"
	"maps"
	"net"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_Collector(t *testing.T) {
	t.Run("single collector no peers", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		reflector := newTestReflector(t)
		devicePK := stringToPubkey("device")
		telemetryProgram := newMemoryTelemetryProgramClient()
		collector := newTestCollector(t, log, devicePK, reflector, []*telemetry.Peer{}, telemetryProgram, 250*time.Millisecond)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			require.NoError(t, collector.Run(ctx))
		}()

		require.Never(t, func() bool {
			return len(telemetryProgram.GetAccounts(t)) > 0
		}, 2*time.Second, 100*time.Millisecond)
	})

	t.Run("shutdown cleanly", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		collector := newTestCollector(t, log, stringToPubkey("device"), newTestReflector(t), []*telemetry.Peer{}, newMemoryTelemetryProgramClient(), 250*time.Millisecond)
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

		device1PK := stringToPubkey("device1")
		device2PK := stringToPubkey("device2")
		device3PK := stringToPubkey("device3")

		link1_2 := stringToPubkey("link1-2")
		link1_3 := stringToPubkey("link1-3")
		link2_1 := stringToPubkey("link2-1")
		link2_3 := stringToPubkey("link2-3")

		ts := time.Now()
		originDevice1Link1_2Key := telemetry.AccountKey{
			OriginDevicePK: device1PK,
			TargetDevicePK: device2PK,
			LinkPK:         link1_2,
			Epoch:          telemetry.DeriveEpoch(ts),
		}
		originDevice1Link1_3Key := telemetry.AccountKey{
			OriginDevicePK: device1PK,
			TargetDevicePK: device3PK,
			LinkPK:         link1_3,
			Epoch:          telemetry.DeriveEpoch(ts),
		}

		originDevice2Link2_1Key := telemetry.AccountKey{
			OriginDevicePK: device2PK,
			TargetDevicePK: device1PK,
			LinkPK:         link2_1,
			Epoch:          telemetry.DeriveEpoch(ts),
		}
		originDevice2Link2_3Key := telemetry.AccountKey{
			OriginDevicePK: device2PK,
			TargetDevicePK: device3PK,
			LinkPK:         link2_3,
			Epoch:          telemetry.DeriveEpoch(ts),
		}

		telemetryProgram1 := newMemoryTelemetryProgramClient()
		collector1 := newTestCollector(t, log.With("runtime", "collector1"), device1PK, reflector1, []*telemetry.Peer{
			{
				DevicePK:   device2PK,
				LinkPK:     link1_2,
				DeviceAddr: reflector2.LocalAddr().(*net.UDPAddr),
			},
			{
				DevicePK:   device3PK,
				LinkPK:     link1_3,
				DeviceAddr: &net.UDPAddr{IP: net.IPv4(10, 241, 1, 3), Port: 1862},
			},
		}, telemetryProgram1, 250*time.Millisecond)

		telemetryProgram2 := newMemoryTelemetryProgramClient()
		collector2 := newTestCollector(t, log.With("runtime", "collector2"), device2PK, reflector2, []*telemetry.Peer{
			{
				DevicePK:   device1PK,
				LinkPK:     link2_1,
				DeviceAddr: reflector1.LocalAddr().(*net.UDPAddr),
			},
			{
				DevicePK:   device3PK,
				LinkPK:     link2_3,
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
			if len(telemetryProgram1.GetAccounts(t)) < 2 || len(telemetryProgram2.GetAccounts(t)) < 2 {
				return false
			}
			for _, samples := range telemetryProgram1.GetAccounts(t) {
				if len(samples) < 2 {
					return false
				}
			}
			for _, samples := range telemetryProgram2.GetAccounts(t) {
				if len(samples) < 2 {
					return false
				}
			}
			return true
		}, 5*time.Second, 100*time.Millisecond)

		// Validate samples from collector1
		accounts := telemetryProgram1.GetAccounts(t)
		require.Len(t, accounts, 2, "expected 2 accounts: %v", maps.Keys(accounts))

		samples1_2 := accounts[originDevice1Link1_2Key]
		require.GreaterOrEqual(t, len(samples1_2), 2)
		for _, s := range samples1_2 {
			require.Greater(t, s.RTT, time.Duration(0))
			require.False(t, s.Loss)
		}

		samples1_3 := accounts[originDevice1Link1_3Key]
		require.GreaterOrEqual(t, len(samples1_3), 2)
		for _, s := range samples1_3 {
			require.Equal(t, s.RTT, time.Duration(0))
			require.True(t, s.Loss)
		}

		// Validate samples from collector2
		accounts = telemetryProgram2.GetAccounts(t)
		require.Len(t, accounts, 2, "expected 2 accounts: %v", maps.Keys(accounts))

		samples2_1 := accounts[originDevice2Link2_1Key]
		require.GreaterOrEqual(t, len(samples2_1), 2)
		for _, s := range samples2_1 {
			require.Greater(t, s.RTT, time.Duration(0))
			require.False(t, s.Loss)
		}

		samples2_3 := accounts[originDevice2Link2_3Key]
		require.GreaterOrEqual(t, len(samples2_3), 2)
		for _, s := range samples2_3 {
			require.Equal(t, s.RTT, time.Duration(0))
			require.True(t, s.Loss)
		}
	})

	t.Run("collector does nothing if no peers are available", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		reflector := newTestReflector(t)
		mockProgram := newMemoryTelemetryProgramClient()
		mockDiscovery := newMockPeerDiscovery()

		mockDiscovery.UpdatePeers(t, []*telemetry.Peer{})

		collector := newTestCollector(t, log, stringToPubkey("device-x"), reflector, []*telemetry.Peer{}, mockProgram, 250*time.Millisecond)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			require.NoError(t, collector.Run(ctx))
		}()

		require.Never(t, func() bool {
			return len(mockProgram.GetAccounts(t)) > 0
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

func newTestCollector(t *testing.T, log *slog.Logger, localDevicePK solana.PublicKey, reflector *twamplight.Reflector, peers []*telemetry.Peer, telemetryProgramClient telemetry.TelemetryProgramClient, submissionInterval time.Duration) *telemetry.Collector {
	peerDiscovery := newMockPeerDiscovery()
	peerDiscovery.UpdatePeers(t, peers)

	collector, err := telemetry.New(log, telemetry.Config{
		LocalDevicePK:          localDevicePK,
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
