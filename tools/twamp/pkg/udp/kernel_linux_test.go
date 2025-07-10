package udp_test

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
	"github.com/stretchr/testify/require"
)

func TestUDP_Dialer_KernelLinux(t *testing.T) {
	dialer, err := udp.NewKernelDialer()
	require.NoError(t, err)

	t.Run("full config succeeds", func(t *testing.T) {
		// Set up listener
		laddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		require.NoError(t, err)
		listener, err := net.ListenUDP("udp", laddr)
		require.NoError(t, err)
		defer listener.Close()

		raddr := listener.LocalAddr().(*net.UDPAddr)

		// Interface name must exist, loopback is safe
		conn, err := dialer.Dial(t.Context(), "lo", laddr, raddr)
		require.NoError(t, err)
		defer conn.Close()

		msg := []byte("test")
		_, err = conn.Write(msg)
		require.NoError(t, err)

		buf := make([]byte, 64)
		err = listener.SetReadDeadline(time.Now().Add(1 * time.Second))
		require.NoError(t, err)
		n, _, err := listener.ReadFromUDP(buf)
		require.NoError(t, err)
		require.Equal(t, "test", string(buf[:n]))
	})

	t.Run("missing remote address should fail", func(t *testing.T) {
		_, err := dialer.Dial(t.Context(), "lo", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, nil)
		require.Error(t, err)
	})

	t.Run("missing local address should not fail", func(t *testing.T) {
		_, err := dialer.Dial(t.Context(), "lo", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})
		require.NoError(t, err)
	})

	t.Run("missing local interface should fail", func(t *testing.T) {
		_, err := dialer.Dial(t.Context(), "", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, nil)
		require.Error(t, err)
	})

	t.Run("invalid interface should fail", func(t *testing.T) {
		raddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:12345")
		_, err := dialer.Dial(t.Context(), "invalid", nil, raddr)
		require.Error(t, err)
		require.ErrorContains(t, err, "failed to dial")
	})
}

func TestUDP_TimestampedReader_KernelLinux(t *testing.T) {
	tests := []struct {
		name        string
		sendMessage string
	}{
		{"basic read", "hello"},
		{"short message", "a"},
		{"empty message", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			log := slog.New(slog.NewTextHandler(nil, nil)).With("test", t.Name())

			addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
			require.NoError(t, err)

			conn, err := net.ListenUDP("udp", addr)
			require.NoError(t, err)
			defer conn.Close()

			reader, err := udp.NewKernelTimestampedReader(log, conn)
			require.NoError(t, err)

			dst := conn.LocalAddr().(*net.UDPAddr)
			sender, err := net.DialUDP("udp", nil, dst)
			require.NoError(t, err)
			defer sender.Close()

			_, err = sender.Write([]byte(tt.sendMessage))
			require.NoError(t, err)

			buf := make([]byte, 512)
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()

			n, ts, err := reader.Read(ctx, buf)
			require.NoError(t, err)
			got := string(buf[:n])
			require.Equal(t, tt.sendMessage, got)
			require.False(t, ts.IsZero())
		})
	}

	t.Run("context timeout", func(t *testing.T) {
		t.Parallel()
		log := slog.New(slog.NewTextHandler(nil, nil)).With("test", t.Name())

		addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		conn, _ := net.ListenUDP("udp", addr)
		defer conn.Close()

		reader, err := udp.NewKernelTimestampedReader(log, conn)
		require.NoError(t, err)

		buf := make([]byte, 512)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, _, err = reader.Read(ctx, buf)
		elapsed := time.Since(start)

		require.Error(t, err)
		require.Greater(t, elapsed, 10*time.Millisecond)
	})

	t.Run("kernel timestamps rarely earlier than user clock", func(t *testing.T) {
		t.Parallel()
		log := slog.New(slog.NewTextHandler(nil, nil)).With("test", t.Name())

		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		require.NoError(t, err)

		conn, err := net.ListenUDP("udp", addr)
		require.NoError(t, err)
		defer conn.Close()

		reader, err := udp.NewKernelTimestampedReader(log, conn)
		require.NoError(t, err)

		dst := conn.LocalAddr().(*net.UDPAddr)
		sender, err := net.DialUDP("udp", nil, dst)
		require.NoError(t, err)
		defer sender.Close()

		const samples = 200
		var negative int
		var zero int
		var positive int

		for i := 0; i < samples; i++ {
			// capture user-space time just before send
			userSend := time.Now()
			_, err := sender.Write([]byte("probe"))
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			buf := make([]byte, 512)
			n, recvTime, err := reader.Read(ctx, buf)
			require.NoError(t, err)
			require.Equal(t, "probe", string(buf[:n]))

			delta := recvTime.Sub(userSend)
			switch {
			case delta < 0:
				negative++
			case delta == 0:
				zero++
			default:
				positive++
			}
		}

		t.Logf("total=%d, negative=%d (%.2f%%), zero=%d, positive=%d",
			samples, negative, float64(negative)*100.0/float64(samples), zero, positive)

		// Assert that negative deltas are rare
		require.Less(t, negative, samples/10, "too many recv timestamps earlier than send time")
		require.Greater(t, positive, 0, "expected some positive deltas")
	})
}
