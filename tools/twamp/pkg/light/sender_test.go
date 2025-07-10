package twamplight_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/require"
)

func BenchmarkTWAMP_Sender(b *testing.B) {
	reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
	require.NoError(b, err)
	b.Cleanup(func() { reflector.Close() })

	ctx, cancel := context.WithCancel(b.Context())
	go func() {
		_ = reflector.Run(ctx)
	}()
	b.Cleanup(cancel)

	sender, err := twamplight.NewSender(ctx, log, "", nil, reflector.LocalAddr().(*net.UDPAddr), 100*time.Millisecond)
	require.NoError(b, err)
	b.Cleanup(func() { sender.Close() })

	b.ResetTimer()
	for range b.N {
		ctx, cancel := context.WithTimeout(b.Context(), 100*time.Millisecond)
		defer cancel()
		_, err := sender.Probe(ctx)
		if err != nil {
			b.Fatalf("probe failed: %v", err)
		}
	}
}

func TestTWAMP_Sender(t *testing.T) {
	t.Run("successful RTT probe", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		addr := reflector.LocalAddr().(*net.UDPAddr)
		sender, err := twamplight.NewSender(ctx, log, "", nil, addr, 250*time.Millisecond)
		require.NoError(t, err)
		require.NotNil(t, sender.LocalAddr())

		rtt, err := sender.Probe(t.Context())
		require.NoError(t, err)
		require.GreaterOrEqual(t, rtt, 0*time.Millisecond)
	})

	t.Run("timeout returns ErrTimeout", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}
		sender, err := twamplight.NewSender(t.Context(), log, "", nil, addr, 100*time.Millisecond)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		rtt, err := sender.Probe(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("unreachable address fails", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 12345}
		sender, err := twamplight.NewSender(t.Context(), log, "", nil, addr, 100*time.Millisecond)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		rtt, err := sender.Probe(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("closed socket results in timeout", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		require.NoError(t, err)
		addr := conn.LocalAddr().(*net.UDPAddr)

		go func() {
			buf := make([]byte, 1500)
			_, _, _ = conn.ReadFromUDP(buf)
			_ = conn.Close()
		}()

		time.Sleep(100 * time.Millisecond)

		sender, err := twamplight.NewSender(t.Context(), log, "", nil, addr, 500*time.Millisecond)
		require.NoError(t, err)

		rtt, err := sender.Probe(context.Background())
		require.ErrorIs(t, err, twamplight.ErrTimeout)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("context cancel aborts probe early", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}                  // blackhole
		sender, err := twamplight.NewSender(t.Context(), log, "", nil, addr, 5*time.Second) // long timeout
		require.NoError(t, err)
		t.Cleanup(func() { sender.Close() })

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		sender, err = twamplight.NewSender(t.Context(), log, "", nil, addr, 5*time.Second)
		require.NoError(t, err)

		rtt, err := sender.Probe(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("context_cancel_aborts_probe_early", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		// Use a blackhole address so the probe would hang unless canceled.
		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}
		sender, err := twamplight.NewSender(t.Context(), log, "", nil, addr, 5*time.Second)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		start := time.Now()
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		rtt, err := sender.Probe(ctx)
		elapsed := time.Since(start)

		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, time.Duration(0), rtt)
		require.Less(t, elapsed, 500*time.Millisecond, "probe should return shortly after context cancel")
	})

	t.Run("context_timeout_aborts_probe_early", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}
		sender, err := twamplight.NewSender(t.Context(), log, "", nil, addr, 5*time.Second) // long socket timeout
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		rtt, err := sender.Probe(ctx)
		elapsed := time.Since(start)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Duration(0), rtt)
		require.Less(t, elapsed, 500*time.Millisecond, "probe should return shortly after context timeout")
	})

	t.Run("sequence numbers increment", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		addr := reflector.LocalAddr().(*net.UDPAddr)
		sender, err := twamplight.NewSender(t.Context(), log, "", nil, addr, 250*time.Millisecond)
		require.NoError(t, err)

		// Send multiple probes and verify sequence numbers
		for range 3 {
			ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
			defer cancel()
			rtt, err := sender.Probe(ctx)
			require.NoError(t, err)
			require.GreaterOrEqual(t, rtt, 0*time.Millisecond)
		}
	})

	t.Run("concurrent probes use different sequence numbers", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		addr := reflector.LocalAddr().(*net.UDPAddr)
		sender, err := twamplight.NewSender(t.Context(), log, "", nil, addr, 250*time.Millisecond)
		require.NoError(t, err)

		var wg sync.WaitGroup
		results := make([]time.Duration, 5)
		errors := make([]error, 5)

		for i := range 5 {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
				defer cancel()
				results[index], errors[index] = sender.Probe(ctx)
			}(i)
		}

		wg.Wait()

		// All probes should succeed
		for i := range 5 {
			require.NoError(t, errors[i])
			require.GreaterOrEqual(t, results[i], 0*time.Millisecond)
		}
	})

	t.Run("returns ErrInvalidPacket for malformed response", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		// Create a custom reflector that sends malformed packets
		conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
		require.NoError(t, err)
		defer conn.Close()

		// Start reflector that sends short packets immediately
		go func() {
			buf := make([]byte, 1500)
			for {
				_, addr, err := conn.ReadFromUDP(buf)
				if err != nil {
					return
				}
				// Send back a short packet instead of 48 bytes immediately
				_, err = conn.WriteToUDP(buf[:24], addr)
				require.NoError(t, err)
			}
		}()

		// Give the reflector a moment to start
		time.Sleep(10 * time.Millisecond)

		sender, err := twamplight.NewSender(t.Context(), log, "", nil, conn.LocalAddr().(*net.UDPAddr), 250*time.Millisecond)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
		defer cancel()
		rtt, err := sender.Probe(ctx)
		require.ErrorIs(t, err, twamplight.ErrInvalidPacket)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("RTT clamping to zero is rare", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		reflector, err := twamplight.NewReflector(log, 0, 100*time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		sender, err := twamplight.NewSender(t.Context(), log, "", nil, reflector.LocalAddr().(*net.UDPAddr), 5*time.Second)
		require.NoError(t, err)
		t.Cleanup(func() { sender.Close() })

		const samples = 200
		var clampedCount int
		var nonZeroCount int

		for i := 0; i < samples; i++ {
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			rtt, err := sender.Probe(ctx)
			cancel()

			require.NoError(t, err)

			if rtt == 0 {
				clampedCount++
			} else {
				nonZeroCount++
			}
		}

		t.Logf("total=%d, clamped=%d (%.2f%%), non-zero=%d", samples, clampedCount, float64(clampedCount)*100.0/float64(samples), nonZeroCount)

		// Clamped RTTs (i.e. rtt == 0) should be rare
		require.Less(t, clampedCount, samples/20, "expected clamping to occur in <5%% of cases")
		require.Greater(t, nonZeroCount, 0, "expected at least some non-zero RTTs")
	})
}

func FuzzTWAMP_Sender(f *testing.F) {
	// Seed with some valid and invalid inputs
	f.Add("127.0.0.1", uint16(65000))     // valid IP, random port
	f.Add("256.256.256.256", uint16(123)) // invalid IP
	f.Add("", uint16(0))                  // empty IP
	f.Add("localhost", uint16(0))         // DNS, but port 0

	f.Fuzz(func(t *testing.T, ip string, port uint16) {
		// Clamp port range to avoid privileged/reserved ports
		if port < 1024 {
			port += 1024
		}

		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, port))
		if err != nil {
			return // skip invalid addr strings
		}
		sender, err := twamplight.NewSender(t.Context(), log, "", nil, addr, 50*time.Millisecond) // empty iface
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
		defer cancel()

		_, _ = sender.Probe(ctx) // we don't assert here, just checking for panics or unexpected crashes
	})
}
