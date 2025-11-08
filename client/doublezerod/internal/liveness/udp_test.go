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
	uc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer uc.Close()

	u, err := NewUDPConn(uc)
	require.NoError(t, err)

	n, err := u.WriteTo([]byte("x"), nil, "", nil)
	require.EqualError(t, err, "nil dst")
	require.Equal(t, 0, n)
}

func TestClient_Liveness_UDP_WriteUDPWithBadIface(t *testing.T) {
	t.Parallel()

	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer srv.Close()

	cl, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer cl.Close()

	w, err := NewUDPConn(cl)
	require.NoError(t, err)

	dst := srv.LocalAddr().(*net.UDPAddr)
	_, err = w.WriteTo([]byte("payload"), dst, "definitely-not-an-interface", nil)
	require.Error(t, err)
}

func TestClient_Liveness_UDP_IPv4RoundtripWriteAndRead(t *testing.T) {
	t.Parallel()

	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer srv.Close()
	_ = srv.SetDeadline(time.Now().Add(2 * time.Second))

	cl, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer cl.Close()
	_ = cl.SetDeadline(time.Now().Add(2 * time.Second))

	r, err := NewUDPConn(srv)
	require.NoError(t, err)
	w, err := NewUDPConn(cl)
	require.NoError(t, err)

	payload := []byte("hello-v4")
	dst := srv.LocalAddr().(*net.UDPAddr)

	nw, err := w.WriteTo(payload, dst, "", nil)
	require.NoError(t, err)
	require.Equal(t, len(payload), nw)

	buf := make([]byte, 128)
	nr, src, dstIP, ifname, err := r.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, len(payload), nr)
	require.Equal(t, payload, buf[:nr])

	require.NotNil(t, src)

	clientLocal := cl.LocalAddr().(*net.UDPAddr)
	serverLocal := srv.LocalAddr().(*net.UDPAddr)

	// Must be the client's IP/port (fails if swapped)
	require.True(t, src.IP.Equal(clientLocal.IP))
	require.Equal(t, clientLocal.Port, src.Port)

	// Must be the server's local IP (fails if swapped)
	require.NotNil(t, dstIP)
	require.True(t, dstIP.Equal(serverLocal.IP))

	// ifname may be empty; if present, it should be loopback
	lb := loopbackInterface(t)
	if ifname != "" {
		require.Equal(t, lb.Name, ifname)
	}
}

func TestClient_Liveness_UDP_WriteUDPWithSrcHintIPv4(t *testing.T) {
	t.Parallel()

	// Binding to 0.0.0.0 then hinting src=127.0.0.1 should still succeed locally.
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer srv.Close()
	_ = srv.SetDeadline(time.Now().Add(2 * time.Second))

	cl, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})
	require.NoError(t, err)
	defer cl.Close()
	_ = cl.SetDeadline(time.Now().Add(2 * time.Second))

	r, err := NewUDPConn(srv)
	require.NoError(t, err)
	w, err := NewUDPConn(cl)
	require.NoError(t, err)

	payload := []byte("src-hint")
	dst := srv.LocalAddr().(*net.UDPAddr)

	nw, err := w.WriteTo(payload, dst, "", net.ParseIP("127.0.0.1"))
	// Some OSes may reject an impossible source; accept either success or specific error, but never hang.
	if err != nil && runtime.GOOS == "windows" {
		t.Skipf("src control message not supported on %s: %v", runtime.GOOS, err)
	}
	if err == nil {
		require.Equal(t, len(payload), nw)

		buf := make([]byte, 128)
		nr, _, _, _, err := r.ReadFrom(buf)
		require.NoError(t, err)
		require.Equal(t, payload, buf[:nr])
	}
}

func TestClient_Liveness_UDP_WriteTo_RejectsIPv6(t *testing.T) {
	t.Parallel()
	uc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer uc.Close()
	u, err := NewUDPConn(uc)
	require.NoError(t, err)
	_, err = u.WriteTo([]byte("x"), &net.UDPAddr{IP: net.ParseIP("::1"), Port: 1}, "", nil)
	require.EqualError(t, err, "ipv6 dst not supported")
}

func TestClient_Liveness_UDP_ReadDeadline_TimesOut(t *testing.T) {
	t.Parallel()
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer srv.Close()
	r, err := NewUDPConn(srv)
	require.NoError(t, err)
	require.NoError(t, r.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
	buf := make([]byte, 8)
	_, _, _, _, err = r.ReadFrom(buf)
	require.Error(t, err)
	nerr, ok := err.(net.Error)
	require.True(t, ok && nerr.Timeout())
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
