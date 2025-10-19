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
	"golang.org/x/sys/unix"
)

// Ensures the listener pinned to loopback replies to echo requests and reports RTTs.
func TestListener_HDRINCL_Loopback_Responds(t *testing.T) {
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

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_ICMP)
	require.NoError(t, err)
	defer unix.Close(fd)

	icmp := make([]byte, 8)
	icmp[0] = 3
	binary.BigEndian.PutUint16(icmp[2:], icmpChecksum(icmp))
	ip := make([]byte, 20+len(icmp))
	ip[0] = 0x45
	ip[9] = 1
	copy(ip[12:16], net.IPv4(127, 0, 0, 1).To4())
	copy(ip[16:20], net.IPv4(127, 0, 0, 1).To4())
	binary.BigEndian.PutUint16(ip[10:], icmpChecksum(ip[:20]))
	copy(ip[20:], icmp)

	require.NoError(t, unix.Sendto(fd, ip, 0, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}))

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
func TestListener_HDRINCL_LargePayload(t *testing.T) {
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

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_ICMP)
	require.NoError(t, err)
	defer unix.Close(fd)

	payload := make([]byte, 4096)
	_, _ = rand.Read(payload)
	req := make([]byte, 8+len(payload))
	req[0] = 8
	req[1] = 0
	binary.BigEndian.PutUint16(req[4:], 0x4242)
	binary.BigEndian.PutUint16(req[6:], 0x0102)
	copy(req[8:], payload)
	binary.BigEndian.PutUint16(req[2:], icmpChecksum(req))

	ip := make([]byte, 20+len(req))
	ip[0] = 0x45
	ip[9] = 1
	copy(ip[12:16], net.IPv4(127, 0, 0, 1).To4())
	copy(ip[16:20], net.IPv4(127, 0, 0, 1).To4())
	binary.BigEndian.PutUint16(ip[10:], icmpChecksum(ip[:20]))
	copy(ip[20:], req)

	require.NoError(t, unix.Sendto(fd, ip, 0, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}))

	s, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer s.Close()
	res, err := s.Send(ctx, SendConfig{Target: net.IPv4(127, 0, 0, 1), Count: 1, Timeout: 800 * time.Millisecond})
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	require.NoError(t, res.Results[0].Error)
}

// Verifies truncated/invalid IPv4/ICMP inputs are ignored and normal operation resumes.
func TestListener_HDRINCL_IgnoresTruncatedJunkAndKeepsWorking(t *testing.T) {
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

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_ICMP)
	require.NoError(t, err)
	defer unix.Close(fd)

	dst := &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}

	require.NoError(t, unix.Sendto(fd, []byte{0x45, 0x00}, 0, dst))

	ip := make([]byte, 20+8)
	ip[0] = 0x45
	ip[9] = 6
	copy(ip[12:16], []byte{127, 0, 0, 1})
	copy(ip[16:20], []byte{127, 0, 0, 1})
	binary.BigEndian.PutUint16(ip[10:], icmpChecksum(ip[:20]))
	require.NoError(t, unix.Sendto(fd, ip, 0, dst))

	ip3 := make([]byte, 20+4)
	ip3[0] = 0x45
	ip3[9] = 1
	copy(ip3[12:16], []byte{127, 0, 0, 1})
	copy(ip3[16:20], []byte{127, 0, 0, 1})
	binary.BigEndian.PutUint16(ip3[10:], icmpChecksum(ip3[:20]))
	require.NoError(t, unix.Sendto(fd, ip3, 0, dst))

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
