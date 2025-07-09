package udp_test

import (
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
	"github.com/stretchr/testify/require"
)

func TestUDP_Dialer_Standard(t *testing.T) {
	dialer := udp.NewStandardDialer()

	t.Run("full config succeeds", func(t *testing.T) {
		// Start a UDP listener to use as a remote endpoint
		laddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		require.NoError(t, err)
		conn, err := net.ListenUDP("udp", laddr)
		require.NoError(t, err)
		defer conn.Close()

		remote := conn.LocalAddr().(*net.UDPAddr)

		// Dial to the local UDP listener
		udpConn, err := dialer.Dial(t.Context(), loopbackInterface(t), laddr, remote)
		require.NoError(t, err)
		defer udpConn.Close()

		// Send and receive a packet to verify connection
		msg := []byte("hello")
		_, err = udpConn.Write(msg)
		require.NoError(t, err)

		buf := make([]byte, 64)
		err = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		require.NoError(t, err)
		n, _, err := conn.ReadFromUDP(buf)
		require.NoError(t, err)
		require.Equal(t, "hello", string(buf[:n]))
	})

	t.Run("missing local address should not fail", func(t *testing.T) {
		_, err := dialer.Dial(t.Context(), loopbackInterface(t), nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})
		require.NoError(t, err)
	})

	t.Run("missing local interface should fail", func(t *testing.T) {
		_, err := dialer.Dial(t.Context(), "", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, nil)
		require.Error(t, err)
	})

	t.Run("invalid interface should fail", func(t *testing.T) {
		_, err := dialer.Dial(t.Context(), "invalid", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})
		require.Error(t, err)
		require.ErrorContains(t, err, "failed to dial")
	})

	t.Run("missing remote address should fail", func(t *testing.T) {
		_, err := dialer.Dial(t.Context(), loopbackInterface(t), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, nil)
		require.Error(t, err)
	})
}

func loopbackInterface(t *testing.T) string {
	t.Helper()

	ifaces, err := net.Interfaces()
	require.NoError(t, err)
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			return iface.Name
		}
	}
	return ""
}
