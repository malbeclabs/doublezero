package twamplight_test

import (
	"context"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/require"
)

func TestTWAMP_Sender_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	runSenderTests(t, func(iface string, localAddr, remoteAddr *net.UDPAddr) (twamplight.Sender, error) {
		return twamplight.NewLinuxSender(t.Context(), iface, localAddr, remoteAddr)
	}, func(addr string) (twamplight.Reflector, error) {
		return twamplight.NewLinuxReflector(addr, 100*time.Millisecond)
	})
}

func TestTWAMP_Sender_Basic(t *testing.T) {
	runSenderTests(t, func(iface string, localAddr, remoteAddr *net.UDPAddr) (twamplight.Sender, error) {
		return twamplight.NewBasicSender(t.Context(), log, iface, localAddr, remoteAddr)
	}, func(addr string) (twamplight.Reflector, error) {
		return twamplight.NewBasicReflector(log, addr, 100*time.Millisecond)
	})
}

func runSenderTests(t *testing.T, newSender func(iface string, localAddr, remoteAddr *net.UDPAddr) (twamplight.Sender, error), newReflector func(addr string) (twamplight.Reflector, error)) {
	t.Run("successful RTT probe", func(t *testing.T) {
		t.Parallel()

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		sender, err := newSender("", nil, reflector.LocalAddr())
		require.NoError(t, err)
		require.NotNil(t, sender.LocalAddr())

		rtt, err := sender.Probe(t.Context())
		require.NoError(t, err)
		require.GreaterOrEqual(t, rtt, 0*time.Millisecond)
	})

	t.Run("timeout returns ErrTimeout", func(t *testing.T) {
		t.Parallel()

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}
		sender, err := newSender("", nil, addr)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		rtt, err := sender.Probe(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("unreachable address fails", func(t *testing.T) {
		t.Parallel()

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 12345}
		sender, err := newSender("", nil, addr)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		rtt, err := sender.Probe(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("closed socket results in timeout", func(t *testing.T) {
		t.Parallel()

		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		require.NoError(t, err)
		addr := conn.LocalAddr().(*net.UDPAddr)

		go func() {
			buf := make([]byte, 1500)
			_, _, _ = conn.ReadFromUDP(buf)
			_ = conn.Close()
		}()

		time.Sleep(100 * time.Millisecond)

		sender, err := newSender("", nil, addr)
		require.NoError(t, err)

		probeCtx, probeCancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer probeCancel()

		rtt, err := sender.Probe(probeCtx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Duration(0), rtt)
	})

	t.Run("context_cancel_does_not_abort_probe_early", func(t *testing.T) {
		t.Parallel()

		// Use a blackhole address so the probe would hang unless canceled.
		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}
		sender, err := newSender("", nil, addr)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		start := time.Now()
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		probeCtx, probeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
		rtt, err := sender.Probe(probeCtx)
		probeCancel()
		elapsed := time.Since(start)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Duration(0), rtt)
		require.GreaterOrEqual(t, elapsed+10*time.Millisecond, 500*time.Millisecond)
	})

	t.Run("default timeout should be configured", func(t *testing.T) {
		t.Parallel()

		// Use a blackhole address so the probe would hang timeout.
		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}
		sender, err := newSender("", nil, addr)
		require.NoError(t, err)

		start := time.Now()

		rtt, err := sender.Probe(t.Context())
		elapsed := time.Since(start)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Duration(0), rtt)
		require.GreaterOrEqual(t, elapsed+10*time.Millisecond, 1*time.Second)
	})

	t.Run("context_timeout_aborts_probe_early", func(t *testing.T) {
		t.Parallel()

		addr := &net.UDPAddr{IP: net.IPv4(10, 255, 255, 255), Port: 65000}
		sender, err := newSender("", nil, addr) // long socket timeout
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

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		addr := reflector.LocalAddr()
		sender, err := newSender("", nil, addr)
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

		reflector, err := newReflector("127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		addr := reflector.LocalAddr()
		sender, err := twamplight.NewBasicSender(t.Context(), log, "", nil, addr)
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

		sender, err := twamplight.NewBasicSender(t.Context(), log, "", nil, conn.LocalAddr().(*net.UDPAddr))
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

		reflector, err := twamplight.NewBasicReflector(log, "127.0.0.1:0", 100*time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { reflector.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { require.NoError(t, reflector.Run(ctx)) }()

		sender, err := twamplight.NewBasicSender(t.Context(), log, "", nil, reflector.LocalAddr())
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

	t.Run("Close stops cleanUpReceived goroutine", func(t *testing.T) {
		t.Parallel()

		before := runtime.NumGoroutine()
		const N = 10

		for range N {
			addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
			sender, err := newSender("", nil, addr)
			require.NoError(t, err)
			sender.Close()
		}

		time.Sleep(200 * time.Millisecond)
		runtime.GC()
		after := runtime.NumGoroutine()

		require.Less(t, after-before, N/2,
			"goroutine count grew by %d after creating and closing %d senders — likely leaking cleanUpReceived goroutines",
			after-before, N)
	})

	t.Run("duplicate packets are ignored", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })

		// Custom reflector that replies twice with the same packet
		go func() {
			buf := make([]byte, 1500)
			for {
				n, addr, err := conn.ReadFromUDP(buf)
				if err != nil {
					return
				}
				_, _ = conn.WriteToUDP(buf[:n], addr)
				time.Sleep(10 * time.Millisecond)
				_, _ = conn.WriteToUDP(buf[:n], addr)
			}
		}()

		sender, err := twamplight.NewBasicSender(t.Context(), log, "", nil, conn.LocalAddr().(*net.UDPAddr))
		require.NoError(t, err)
		t.Cleanup(func() { sender.Close() })

		ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
		defer cancel()

		// Send several probes with short delay between them and check that the RTT is non-zero.

		rtt, err := sender.Probe(ctx)
		require.NoError(t, err)
		require.Greater(t, rtt, 0*time.Millisecond)

		time.Sleep(100 * time.Millisecond)
		rtt, err = sender.Probe(ctx)
		require.NoError(t, err)
		require.Greater(t, rtt, 0*time.Millisecond)

		time.Sleep(100 * time.Millisecond)
		rtt, err = sender.Probe(ctx)
		require.NoError(t, err)
		require.Greater(t, rtt, 0*time.Millisecond)
	})

}
