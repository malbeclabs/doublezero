package udp_test

import (
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
	"github.com/stretchr/testify/require"
)

func TestUDP_Dialer_Kernel(t *testing.T) {
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
