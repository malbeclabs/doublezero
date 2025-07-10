package udp_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
	"github.com/stretchr/testify/require"
)

func TestUDP_TimestampedReader_Wallclock(t *testing.T) {
	t.Run("SuccessfulRead", func(t *testing.T) {
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("ResolveUDPAddr failed: %v", err)
		}
		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			t.Fatalf("ListenUDP failed: %v", err)
		}
		defer conn.Close()

		reader := udp.NewWallclockTimestampedReader(conn)
		buf := make([]byte, 512)

		sender, err := net.DialUDP("udp", nil, conn.LocalAddr().(*net.UDPAddr))
		require.NoError(t, err)
		defer sender.Close()

		want := []byte("test-message")
		_, err = sender.Write(want)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		n, ts, err := reader.Read(ctx, buf)
		require.NoError(t, err)
		got := buf[:n]
		require.Equal(t, string(want), string(got))
		require.False(t, ts.IsZero())
	})

	t.Run("Timeout", func(t *testing.T) {
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		require.NoError(t, err)
		conn, err := net.ListenUDP("udp", addr)
		require.NoError(t, err)
		defer conn.Close()

		reader := udp.NewWallclockTimestampedReader(conn)
		buf := make([]byte, 512)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, _, err = reader.Read(ctx, buf)
		elapsed := time.Since(start)

		require.Error(t, err)
		require.Greater(t, elapsed, 10*time.Millisecond)
	})
}
