//go:build linux

package uping

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

// Ensures the listener pinned to loopback replies to echo requests and reports RTTs.
func TestListener_Loopback_Responds(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	l, err := NewListener(ListenerConfig{
		Interface: "lo",
		IP:        net.IPv4(127, 0, 0, 1),
		Timeout:   200 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	go func() { _ = l.Listen(ctx) }()
	time.Sleep(40 * time.Millisecond)

	s, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer s.Close()

	res, err := s.Send(ctx, SendConfig{
		Target:  net.IPv4(127, 0, 0, 1),
		Count:   2,
		Timeout: 600 * time.Millisecond,
	})
	require.NoError(t, err)
	require.Len(t, res.Results, 2)
	for i, r := range res.Results {
		require.NoErrorf(t, r.Error, "i=%d", i)
		require.Greater(t, r.RTT, time.Duration(0))
	}
}

// Verifies the listener exits promptly when the context is cancelled.
func TestListener_ContextCancel_Exits(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	l, err := NewListener(ListenerConfig{
		Interface: "lo",
		IP:        net.IPv4(127, 0, 0, 1),
		Timeout:   150 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { _ = l.Listen(ctx); close(done) }()
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("listener did not exit after cancel")
	}
}

// Confirms non-echo ICMP is ignored and that subsequent valid echo still gets a reply.
func TestListener_Ignores_NonEcho_Then_Replies(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	l, err := NewListener(ListenerConfig{
		Interface: "lo",
		IP:        net.IPv4(127, 0, 0, 1),
		Timeout:   200 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	go func() { _ = l.Listen(ctx) }()
	time.Sleep(40 * time.Millisecond)

	// Inject a non-echo ICMP (dest unreachable) using ipv4.PacketConn.
	c, err := net.ListenIP("ip4:icmp", &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	ip4c := ipv4.NewPacketConn(c)
	defer func() { _ = ip4c.Close(); _ = c.Close() }()
	_ = ip4c.SetTTL(64)

	nonEcho := &icmp.Message{Type: ipv4.ICMPTypeDestinationUnreachable, Code: 0, Body: &icmp.DstUnreach{}}
	nb, err := nonEcho.Marshal(nil)
	require.NoError(t, err)
	_, err = ip4c.WriteTo(nb, &ipv4.ControlMessage{IfIndex: 1, Src: net.IPv4(127, 0, 0, 1)}, &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)

	// Now a real echo via our Sender should still get a reply.
	s, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer s.Close()

	res, err := s.Send(ctx, SendConfig{
		Target:  net.IPv4(127, 0, 0, 1),
		Count:   1,
		Timeout: 600 * time.Millisecond,
	})
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	require.NoError(t, res.Results[0].Error)
}

// Validates config error paths for missing iface/IP and invalid timeout.
func TestListenerConfig_Validate_Errors(t *testing.T) {
	t.Parallel()

	_, err := NewListener(ListenerConfig{IP: net.IPv4(127, 0, 0, 1), Timeout: time.Second})
	require.Error(t, err)

	_, err = NewListener(ListenerConfig{Interface: "lo", Timeout: time.Second})
	require.Error(t, err)
	_, err = NewListener(ListenerConfig{Interface: "lo", IP: net.IPv6loopback, Timeout: time.Second})
	require.Error(t, err)

	cfg := ListenerConfig{Interface: "lo", IP: net.IPv4(127, 0, 0, 1), Timeout: -time.Second}
	require.Error(t, cfg.Validate())
}

// Exercises large ICMP payloads and ensures the listener continues to reply.
func TestListener_LargePayload(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	l, err := NewListener(ListenerConfig{
		Interface: "lo",
		IP:        net.IPv4(127, 0, 0, 1),
		Timeout:   200 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _ = l.Listen(ctx) }()
	time.Sleep(40 * time.Millisecond)

	// Send a large echo request using ipv4.PacketConn to 127.0.0.1.
	c, err := net.ListenIP("ip4:icmp", &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	ip4c := ipv4.NewPacketConn(c)
	defer func() { _ = ip4c.Close(); _ = c.Close() }()
	_ = ip4c.SetTTL(64)

	payload := make([]byte, 4096)
	_, _ = rand.Read(payload)
	msg := &icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{ID: 0x4242, Seq: 0x0102, Data: payload},
	}
	wb, err := msg.Marshal(nil)
	require.NoError(t, err)
	_, err = ip4c.WriteTo(wb, &ipv4.ControlMessage{IfIndex: 1, Src: net.IPv4(127, 0, 0, 1)}, &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)

	// Confirm we still get a reply using the Sender path.
	s, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer s.Close()
	res, err := s.Send(ctx, SendConfig{Target: net.IPv4(127, 0, 0, 1), Count: 1, Timeout: 800 * time.Millisecond})
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	require.NoError(t, res.Results[0].Error)
}

// Verifies truncated/invalid IPv4/ICMP inputs are ignored and normal operation resumes.
func TestListener_IgnoresTruncatedJunkAndKeepsWorking(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	l, err := NewListener(ListenerConfig{
		Interface: "lo",
		IP:        net.IPv4(127, 0, 0, 1),
		Timeout:   150 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = l.Listen(ctx) }()
	time.Sleep(40 * time.Millisecond)

	// For malformed frames, use raw unix socket (ipv4.PacketConn won’t craft broken IP).
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_ICMP)
	require.NoError(t, err)
	defer unix.Close(fd)

	dst := &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}

	// Truncated IP header
	require.NoError(t, unix.Sendto(fd, []byte{0x45, 0x00}, 0, dst))

	// Non-ICMP protocol in IP header
	ip := make([]byte, 20+8)
	ip[0] = 0x45
	ip[9] = 6
	copy(ip[12:16], []byte{127, 0, 0, 1})
	copy(ip[16:20], []byte{127, 0, 0, 1})
	binary.BigEndian.PutUint16(ip[10:], icmpChecksum(ip[:20]))
	require.NoError(t, unix.Sendto(fd, ip, 0, dst))

	// Too-short ICMP payload
	ip2 := make([]byte, 20+4)
	ip2[0] = 0x45
	ip2[9] = 1
	copy(ip2[12:16], []byte{127, 0, 0, 1})
	copy(ip2[16:20], []byte{127, 0, 0, 1})
	binary.BigEndian.PutUint16(ip2[10:], icmpChecksum(ip2[:20]))
	require.NoError(t, unix.Sendto(fd, ip2, 0, dst))

	// Normal echo still works afterward.
	s, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer s.Close()
	res, err := s.Send(ctx, SendConfig{Target: net.IPv4(127, 0, 0, 1), Count: 1, Timeout: 600 * time.Millisecond})
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	require.NoError(t, res.Results[0].Error)
}

// Ensures echo requests with bad ICMP checksums are ignored; normal echo still works afterward.
func TestListener_Ignores_BadICMPChecksum_Then_Replies(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	l, err := NewListener(ListenerConfig{
		Interface: "lo",
		IP:        net.IPv4(127, 0, 0, 1),
		Timeout:   200 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	go func() { _ = l.Listen(ctx) }()
	time.Sleep(40 * time.Millisecond)

	// Craft an echo with an intentionally bad checksum; inject via raw unix.
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_ICMP)
	require.NoError(t, err)
	defer unix.Close(fd)

	payload := make([]byte, 64)
	_, _ = rand.Read(payload)
	req := make([]byte, 8+len(payload))
	req[0] = 8
	req[1] = 0
	binary.BigEndian.PutUint16(req[4:], 0xBEEF)
	binary.BigEndian.PutUint16(req[6:], 0x0001)
	copy(req[8:], payload)
	sum := icmpChecksum(req)
	sum ^= 0x00FF
	binary.BigEndian.PutUint16(req[2:], sum)

	ip := make([]byte, 20+len(req))
	ip[0] = 0x45
	ip[9] = 1
	copy(ip[12:16], net.IPv4(127, 0, 0, 1).To4())
	copy(ip[16:20], net.IPv4(127, 0, 0, 1).To4())
	binary.BigEndian.PutUint16(ip[:20][10:], icmpChecksum(ip[:20]))
	copy(ip[20:], req)

	require.NoError(t, unix.Sendto(fd, ip, 0, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}))

	// Normal echo afterwards.
	s, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer s.Close()
	res, err := s.Send(ctx, SendConfig{Target: net.IPv4(127, 0, 0, 1), Count: 1, Timeout: 800 * time.Millisecond})
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	require.NoError(t, res.Results[0].Error)
}

// Validates pollTimeoutMs against deadline/fallback edge cases and infinite mode.
func Test_pollTimeoutMs(t *testing.T) {
	t.Parallel()

	{
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		ms := pollTimeoutMs(ctx, 500*time.Millisecond)
		require.InDelta(t, 50, ms, 25)
	}

	{
		ctx := context.Background()
		ms := pollTimeoutMs(ctx, 123*time.Millisecond)
		require.InDelta(t, 123, ms, 10)
	}

	{
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		time.Sleep(200 * time.Microsecond)
		defer cancel()
		ms := pollTimeoutMs(ctx, 5*time.Second)
		require.Equal(t, 0, ms)
	}

	{
		ctx := context.Background()
		ms := pollTimeoutMs(ctx, 0)
		require.Equal(t, -1, ms)
	}
}
