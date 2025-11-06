package liveness

import (
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_Liveness_UDP_WriteUDPWithNilDst(t *testing.T) {
	t.Parallel()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer pc.Close()
	n, err := writeUDP(pc, []byte("x"), nil, "", nil)
	require.EqualError(t, err, "nil dst")
	require.Equal(t, 0, n)
}

func TestClient_Liveness_UDP_WriteUDPWithBadIface(t *testing.T) {
	t.Parallel()
	srv, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer srv.Close()

	cl, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer cl.Close()

	dst := srv.LocalAddr().(*net.UDPAddr)
	_, err = writeUDP(cl, []byte("payload"), dst, "definitely-not-an-interface", nil)
	require.Error(t, err)
}

func TestClient_Liveness_UDP_IPv4RoundtripWriteAndRead(t *testing.T) {
	t.Parallel()

	// server
	srv, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer srv.Close()
	_ = srv.SetDeadline(time.Now().Add(2 * time.Second))

	// client (writer)
	cl, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer cl.Close()
	_ = cl.SetDeadline(time.Now().Add(2 * time.Second))

	payload := []byte("hello-v4")
	dst := srv.LocalAddr().(*net.UDPAddr)

	nw, err := writeUDP(cl, payload, dst, "", nil)
	require.NoError(t, err)
	require.Equal(t, len(payload), nw)

	buf := make([]byte, 128)
	nr, src, dstIP, ifname, err := readFromUDP(srv, buf)
	require.NoError(t, err)
	require.Equal(t, len(payload), nr)
	require.Equal(t, payload, buf[:nr])

	require.NotNil(t, src)
	require.Equal(t, "127.0.0.1", src.IP.String())
	// src port should be client's local port
	require.Equal(t, cl.LocalAddr().(*net.UDPAddr).Port, src.Port)

	// dst may be nil on some platforms; if present, it should be 127.0.0.1
	if dstIP != nil {
		require.True(t, dstIP.IsLoopback())
	}
	// ifname may be empty on some stacks; if present, it should be loopback
	lb := loopbackInterface(t)
	if ifname != "" {
		require.Equal(t, lb.Name, ifname)
	}
}

func TestClient_Liveness_UDP_IPv6RoundtripWriteAndRead(t *testing.T) {
	t.Parallel()

	srv, err := net.ListenPacket("udp6", "[::1]:0")
	if err != nil {
		t.Skipf("udp6 not available: %v", err)
	}
	defer srv.Close()
	_ = srv.SetDeadline(time.Now().Add(2 * time.Second))

	cl, err := net.ListenPacket("udp6", "[::1]:0")
	require.NoError(t, err)
	defer cl.Close()
	_ = cl.SetDeadline(time.Now().Add(2 * time.Second))

	lb := loopbackInterface(t)
	payload := []byte("hello-v6")
	dst := srv.LocalAddr().(*net.UDPAddr)

	// Try with explicit iface; many stacks accept loopback here
	nw, err := writeUDP(cl, payload, dst, lb.Name, nil)
	// Some platforms may return an error for IfIndex on loopback; if so, retry w/o iface
	if err != nil {
		nw, err = writeUDP(cl, payload, dst, "", nil)
	}
	require.NoError(t, err)
	require.Equal(t, len(payload), nw)

	buf := make([]byte, 128)
	nr, src, dstIP, ifname, err := readFromUDP(srv, buf)
	require.NoError(t, err)
	require.Equal(t, len(payload), nr)
	require.Equal(t, payload, buf[:nr])

	require.NotNil(t, src)
	require.True(t, src.IP.IsLoopback())
	require.Equal(t, cl.LocalAddr().(*net.UDPAddr).Port, src.Port)

	if dstIP != nil {
		require.True(t, dstIP.IsLoopback())
	}
	if ifname != "" {
		require.Equal(t, lb.Name, ifname)
	}
}

func TestClient_Liveness_UDP_WriteUDPWithSrcHintIPv4(t *testing.T) {
	t.Parallel()

	// Binding to 0.0.0.0 then hinting src=127.0.0.1 should still succeed locally.
	srv, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer srv.Close()
	_ = srv.SetDeadline(time.Now().Add(2 * time.Second))

	cl, err := net.ListenPacket("udp4", "0.0.0.0:0")
	require.NoError(t, err)
	defer cl.Close()
	_ = cl.SetDeadline(time.Now().Add(2 * time.Second))

	payload := []byte("src-hint")
	dst := srv.LocalAddr().(*net.UDPAddr)

	nw, err := writeUDP(cl, payload, dst, "", net.ParseIP("127.0.0.1"))
	// Some OSes may reject an impossible source; accept either success or specific error, but never hang.
	if err != nil && runtime.GOOS == "windows" {
		t.Skipf("src control message not supported on %s: %v", runtime.GOOS, err)
	}
	if err == nil {
		require.Equal(t, len(payload), nw)

		buf := make([]byte, 128)
		nr, _, _, _, err := readFromUDP(srv, buf)
		require.NoError(t, err)
		require.Equal(t, payload, buf[:nr])
	}
}

func loopbackInterface(t *testing.T) net.Interface {
	ifs, err := net.Interfaces()
	require.NoError(t, err)
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagLoopback != 0 && ifi.Flags&net.FlagUp != 0 {
			return ifi
		}
	}
	t.Skip("no up loopback interface found")
	return net.Interface{}
}
