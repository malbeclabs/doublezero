package telemetry_test

import (
	"context"
	"log/slog"
	"maps"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
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

		epoch := uint64(100)
		originDevice1Link1_2Key := telemetry.PartitionKey{
			OriginDevicePK: device1PK,
			TargetDevicePK: device2PK,
			LinkPK:         link1_2,
			Epoch:          epoch,
		}
		originDevice1Link1_3Key := telemetry.PartitionKey{
			OriginDevicePK: device1PK,
			TargetDevicePK: device3PK,
			LinkPK:         link1_3,
			Epoch:          epoch,
		}

		originDevice2Link2_1Key := telemetry.PartitionKey{
			OriginDevicePK: device2PK,
			TargetDevicePK: device1PK,
			LinkPK:         link2_1,
			Epoch:          epoch,
		}
		originDevice2Link2_3Key := telemetry.PartitionKey{
			OriginDevicePK: device2PK,
			TargetDevicePK: device3PK,
			LinkPK:         link2_3,
			Epoch:          epoch,
		}

		telemetryProgram1 := newMemoryTelemetryProgramClient()
		collector1 := newTestCollector(t, log.With("runtime", "collector1"), device1PK, reflector1, []*telemetry.Peer{
			{
				DevicePK: device2PK,
				LinkPK:   link1_2,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  reflector1.LocalAddr().IP,
					TargetIP:  reflector2.LocalAddr().IP,
				},
				TWAMPPort: uint16(reflector2.LocalAddr().Port),
			},
			{
				DevicePK: device3PK,
				LinkPK:   link1_3,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.IPv4(10, 241, 1, 2),
					TargetIP:  net.IPv4(10, 241, 1, 3),
				},
				TWAMPPort: 1862,
			},
		}, telemetryProgram1, 250*time.Millisecond)

		telemetryProgram2 := newMemoryTelemetryProgramClient()
		collector2 := newTestCollector(t, log.With("runtime", "collector2"), device2PK, reflector2, []*telemetry.Peer{
			{
				DevicePK: device1PK,
				LinkPK:   link2_1,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  reflector2.LocalAddr().IP,
					TargetIP:  reflector1.LocalAddr().IP,
				},
				TWAMPPort: uint16(reflector2.LocalAddr().Port),
			},
			{
				DevicePK: device3PK,
				LinkPK:   link2_3,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.IPv4(10, 241, 1, 2),
					TargetIP:  net.IPv4(10, 241, 1, 3),
				},
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

	t.Run("updates_sender_when_peer_address_changes", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		devicePK := stringToPubkey("device1")
		peerPK := stringToPubkey("device2")
		linkPK := stringToPubkey("link1-2")

		reflector := newTestReflector(t)
		telemetryProgram := newMemoryTelemetryProgramClient()

		peerDiscovery := newMockPeerDiscovery()

		// Initially peer points to valid reflector
		peerDiscovery.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.ParseIP("127.0.0.1"),
					TargetIP:  reflector.LocalAddr().IP,
				},
				TWAMPPort: uint16(reflector.LocalAddr().Port),
			},
		})

		collector, err := telemetry.New(log, telemetry.Config{
			LocalDevicePK:           devicePK,
			ProbeInterval:           100 * time.Millisecond,
			SubmissionInterval:      250 * time.Millisecond,
			TWAMPSenderTimeout:      250 * time.Millisecond,
			SenderTTL:               1 * time.Millisecond,
			SubmitterMaxConcurrency: 10,
			TWAMPReflector:          reflector,
			PeerDiscovery:           peerDiscovery,
			TelemetryProgramClient:  telemetryProgram,
			GetCurrentEpochFunc: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			require.NoError(t, collector.Run(ctx))
		}()

		epoch := uint64(100)

		accountKey := telemetry.PartitionKey{
			OriginDevicePK: devicePK,
			TargetDevicePK: peerPK,
			LinkPK:         linkPK,
			Epoch:          epoch,
		}

		// Wait for successful RTT submission
		require.Eventually(t, func() bool {
			samples := telemetryProgram.GetAccounts(t)[accountKey]
			return len(samples) > 0 && !samples[len(samples)-1].Loss
		}, 3*time.Second, 100*time.Millisecond, "should have working sender with real address")

		// Simulate address change to non-working peer
		peerDiscovery.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.ParseIP("127.0.0.1"),
					TargetIP:  net.IPv4(203, 0, 113, 1),
				},
				TWAMPPort: 9999,
			},
		})

		// Wait for RTT to show packet loss (indicating sender was updated and is now failing)
		require.Eventually(t, func() bool {
			samples := telemetryProgram.GetAccounts(t)[accountKey]
			for _, s := range samples {
				if s.Loss {
					return true
				}
			}
			return false
		}, 5*time.Second, 100*time.Millisecond, "should reflect new address in updated sender")

		// Simulate reverting to valid address
		peerDiscovery.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.IPv4(10, 241, 1, 1),
					TargetIP:  reflector.LocalAddr().IP,
				},
				TWAMPPort: uint16(reflector.LocalAddr().Port),
			},
		})

		// Wait for RTT to resume with success (no packet loss)
		require.Eventually(t, func() bool {
			samples := telemetryProgram.GetAccounts(t)[accountKey]
			for _, s := range samples {
				if !s.Loss {
					return true
				}
			}
			return false
		}, 5*time.Second, 100*time.Millisecond, "should resume working after peer address is fixed")

	})

	t.Run("deduplicates senders for semantically equal peers", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		reflector := newTestReflector(t)
		devicePK := stringToPubkey("device")
		peerPK := stringToPubkey("peer")
		linkPK := stringToPubkey("link")

		// Two distinct *Peer values with equivalent content
		peer1 := &telemetry.Peer{
			DevicePK: peerPK,
			LinkPK:   linkPK,
			Tunnel: &netutil.LocalTunnel{
				Interface: loopbackInterface(t),
				SourceIP:  reflector.LocalAddr().IP,
				TargetIP:  reflector.LocalAddr().IP,
			},
			TWAMPPort: uint16(reflector.LocalAddr().Port),
		}
		peer2 := &telemetry.Peer{
			DevicePK: peerPK,
			LinkPK:   linkPK,
			Tunnel: &netutil.LocalTunnel{
				Interface: loopbackInterface(t),
				SourceIP:  reflector.LocalAddr().IP,
				TargetIP:  reflector.LocalAddr().IP,
			},
			TWAMPPort: uint16(reflector.LocalAddr().Port),
		}

		require.Equal(t, peer1.String(), peer2.String())

		program := newMemoryTelemetryProgramClient()
		collector := newTestCollector(t, log, devicePK, reflector, []*telemetry.Peer{peer1, peer2}, program, 250*time.Millisecond)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			require.NoError(t, collector.Run(ctx))
		}()

		epoch := uint64(100)

		key := telemetry.PartitionKey{
			OriginDevicePK: devicePK,
			TargetDevicePK: peerPK,
			LinkPK:         linkPK,
			Epoch:          epoch,
		}

		// Wait for multiple samples
		require.Eventually(t, func() bool {
			return len(program.GetAccounts(t)[key]) >= 3
		}, 4*time.Second, 100*time.Millisecond)

		// Assert that only one account was used (i.e., peer.String() deduplicated senders)
		require.Len(t, program.GetAccounts(t), 1, "expected only one sender to be used for logically equal peers")
	})

	t.Run("evicts_sender_after_consecutive_losses", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		devicePK := stringToPubkey("device-evict")
		peerPK := stringToPubkey("peer-evict")
		linkPK := stringToPubkey("link-evict")

		reflector := newTestReflector(t)
		telemetryProgram := newMemoryTelemetryProgramClient()
		peerDiscovery := newMockPeerDiscovery()

		// Start with a working peer pointing to the real reflector.
		peerDiscovery.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.ParseIP("127.0.0.1"),
					TargetIP:  reflector.LocalAddr().IP,
				},
				TWAMPPort: uint16(reflector.LocalAddr().Port),
			},
		})

		maxLosses := 5
		collector, err := telemetry.New(log, telemetry.Config{
			LocalDevicePK:              devicePK,
			ProbeInterval:              50 * time.Millisecond,
			SubmissionInterval:         200 * time.Millisecond,
			TWAMPSenderTimeout:         100 * time.Millisecond,
			SenderTTL:                  10 * time.Minute, // long TTL so eviction is only via losses
			SubmitterMaxConcurrency:    10,
			TWAMPReflector:             reflector,
			PeerDiscovery:              peerDiscovery,
			TelemetryProgramClient:     telemetryProgram,
			GetCurrentEpochFunc:        func(ctx context.Context) (uint64, error) { return 100, nil },
			MaxConsecutiveSenderLosses: maxLosses,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, collector.Run(ctx)) }()

		accountKey := telemetry.PartitionKey{
			OriginDevicePK: devicePK,
			TargetDevicePK: peerPK,
			LinkPK:         linkPK,
			Epoch:          100,
		}

		// 1) Verify we get successful RTT samples.
		require.Eventually(t, func() bool {
			s := telemetryProgram.GetAccounts(t)[accountKey]
			for _, sample := range s {
				if !sample.Loss {
					return true
				}
			}
			return false
		}, 3*time.Second, 50*time.Millisecond, "should collect successful RTT samples")

		// 2) Switch to a blackhole address to trigger consecutive losses.
		peerDiscovery.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.ParseIP("127.0.0.1"),
					TargetIP:  net.IPv4(203, 0, 113, 1),
				},
				TWAMPPort: 9999,
			},
		})

		// Wait for losses to accumulate and be submitted.
		require.Eventually(t, func() bool {
			s := telemetryProgram.GetAccounts(t)[accountKey]
			lossCount := 0
			for _, sample := range s {
				if sample.Loss {
					lossCount++
				}
			}
			return lossCount >= maxLosses
		}, 5*time.Second, 50*time.Millisecond, "should observe losses after switching to blackhole")

		// 3) Switch back to working address. Because the sender was evicted (not
		// waiting for TTL), a new sender should be created immediately for the
		// working address, and we should see successful probes resume.
		peerDiscovery.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.ParseIP("127.0.0.1"),
					TargetIP:  reflector.LocalAddr().IP,
				},
				TWAMPPort: uint16(reflector.LocalAddr().Port),
			},
		})

		// Verify success resumes without needing to wait for TTL.
		require.Eventually(t, func() bool {
			s := telemetryProgram.GetAccounts(t)[accountKey]
			if len(s) == 0 {
				return false
			}
			// Check the tail for a recent non-loss sample.
			for i := len(s) - 1; i >= 0 && i >= len(s)-6; i-- {
				if !s[i].Loss {
					return true
				}
			}
			return false
		}, 5*time.Second, 50*time.Millisecond, "should resume success after sender eviction and address fix")
	})

	t.Run("recreates_sender_after_ttl", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())
		devicePK := stringToPubkey("device-ttl")
		peerPK := stringToPubkey("peer-ttl")
		linkPK := stringToPubkey("link-ttl")

		// Start a real reflector
		reflector := newTestReflector(t)
		telemetryProgram := newMemoryTelemetryProgramClient()
		peerDiscovery := newMockPeerDiscovery()

		// Controlled clock for TTL checks
		now := time.Now()
		var nowMu sync.Mutex
		advance := func(d time.Duration) {
			nowMu.Lock()
			now = now.Add(d)
			nowMu.Unlock()
		}

		// Initially: working address (points to real reflector)
		peerDiscovery.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.ParseIP("127.0.0.1"),
					TargetIP:  reflector.LocalAddr().IP,
				},
				TWAMPPort: uint16(reflector.LocalAddr().Port),
			},
		})

		// Small TTL so we can force rotation quickly; run fast probes.
		collector, err := telemetry.New(log, telemetry.Config{
			LocalDevicePK:           devicePK,
			ProbeInterval:           75 * time.Millisecond,
			SubmissionInterval:      200 * time.Millisecond,
			TWAMPSenderTimeout:      200 * time.Millisecond,
			SubmitterMaxConcurrency: 10,
			TWAMPReflector:          reflector,
			PeerDiscovery:           peerDiscovery,
			TelemetryProgramClient:  telemetryProgram,
			GetCurrentEpochFunc:     func(ctx context.Context) (uint64, error) { return 100, nil },
			SenderTTL:               250 * time.Millisecond,
			NowFunc: func() time.Time {
				nowMu.Lock()
				defer nowMu.Unlock()
				return now
			},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, collector.Run(ctx)) }()

		accountKey := telemetry.PartitionKey{
			OriginDevicePK: devicePK,
			TargetDevicePK: peerPK,
			LinkPK:         linkPK,
			Epoch:          100,
		}

		// 1) Prove we have a working sender (success, not loss).
		require.Eventually(t, func() bool {
			s := telemetryProgram.GetAccounts(t)[accountKey]
			return len(s) > 0 && !s[len(s)-1].Loss
		}, 3*time.Second, 50*time.Millisecond, "should collect successful RTT samples first")

		// 2) Change the peer to a non-working address BUT only expect the failure
		// after TTL has elapsed, which requires the collector to recreate the sender.
		peerDiscovery.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.ParseIP("127.0.0.1"),
					TargetIP:  net.IPv4(203, 0, 113, 1), // blackhole test-net IP
				},
				TWAMPPort: 9999,
			},
		})

		// Fast-forward logical time beyond TTL so getOrCreateSender rotates.
		advance(300 * time.Millisecond)

		require.Eventually(t, func() bool {
			s := telemetryProgram.GetAccounts(t)[accountKey]
			if len(s) == 0 {
				return false
			}
			// expect to observe at least one loss after rotation
			for i := range s {
				if s[i].Loss {
					return true
				}
			}
			return false
		}, 4*time.Second, 50*time.Millisecond, "should observe loss after TTL forces sender recreation")

		// 3) Restore a working address; advance time again so the stale/failing sender
		// is rotated to a working one, and verify we resume success.
		peerDiscovery.UpdatePeers(t, []*telemetry.Peer{
			{
				DevicePK: peerPK,
				LinkPK:   linkPK,
				Tunnel: &netutil.LocalTunnel{
					Interface: loopbackInterface(t),
					SourceIP:  net.IPv4(10, 241, 1, 1),
					TargetIP:  reflector.LocalAddr().IP,
				},
				TWAMPPort: uint16(reflector.LocalAddr().Port),
			},
		})
		advance(300 * time.Millisecond)

		require.Eventually(t, func() bool {
			s := telemetryProgram.GetAccounts(t)[accountKey]
			if len(s) == 0 {
				return false
			}
			// look for a non-loss sample after the revert
			for i := len(s) - 1; i >= 0 && i >= len(s)-6; i-- { // check tail
				if !s[i].Loss {
					return true
				}
			}
			return false
		}, 4*time.Second, 50*time.Millisecond, "should resume success after TTL rotation to working address")
	})
}

func newTestReflector(t *testing.T) twamplight.Reflector {
	reflector, err := twamplight.NewReflector(log, "127.0.0.1:0", 1*time.Second)
	require.NoError(t, err)

	t.Cleanup(func() {
		reflector.Close()
	})

	return reflector
}

func newTestCollector(t *testing.T, log *slog.Logger, localDevicePK solana.PublicKey, reflector twamplight.Reflector, peers []*telemetry.Peer, telemetryProgramClient telemetry.TelemetryProgramClient, submissionInterval time.Duration) *telemetry.Collector {
	peerDiscovery := newMockPeerDiscovery()
	peerDiscovery.UpdatePeers(t, peers)

	collector, err := telemetry.New(log, telemetry.Config{
		LocalDevicePK:           localDevicePK,
		ProbeInterval:           100 * time.Millisecond,
		SubmissionInterval:      submissionInterval,
		TWAMPSenderTimeout:      1 * time.Second,
		SenderTTL:               1 * time.Millisecond,
		SubmitterMaxConcurrency: 10,
		TWAMPReflector:          reflector,
		PeerDiscovery:           peerDiscovery,
		TelemetryProgramClient:  telemetryProgramClient,
		GetCurrentEpochFunc: func(ctx context.Context) (uint64, error) {
			return 100, nil
		},
	})
	require.NoError(t, err)

	return collector
}
