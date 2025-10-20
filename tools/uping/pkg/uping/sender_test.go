//go:build linux

package uping

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Verifies ICMP echo packet construction and checksum correctness.
func TestUping_Sender_ChecksumAndICMPEcho(t *testing.T) {
	t.Parallel()
	id, seq := uint16(0x1234), uint16(0x9abc)
	p := icmpEcho(id, seq, []byte{1, 2, 3, 4, 5})
	require.Equal(t, 13, len(p))
	require.Equal(t, byte(8), p[0])
	got := binary.BigEndian.Uint16(p[2:4])
	binary.BigEndian.PutUint16(p[2:4], 0)
	require.Equal(t, icmpChecksum(p), got)
}

// Confirms that validateEchoReply correctly detects valid ICMP echo replies.
func TestUping_Sender_ValidateEchoReply(t *testing.T) {
	t.Parallel()
	src := net.IPv4(10, 1, 2, 3).To4()
	dst := net.IPv4(10, 9, 9, 9).To4()
	id, seq, nonce := uint16(0x42), uint16(7), uint64(0xdeadbeefcafebabe)
	req := icmpEcho(id, seq, func() []byte { b := make([]byte, 8); binary.BigEndian.PutUint64(b, nonce); return b }())
	rep := make([]byte, 20+len(req))
	rep[0] = 0x45
	copy(rep[12:16], src)
	copy(rep[16:20], dst)
	rep[9] = 1
	binary.BigEndian.PutUint16(rep[10:], icmpChecksum(rep[:20]))
	icmp := rep[20:]
	copy(icmp, req)
	icmp[0] = 0
	binary.BigEndian.PutUint16(icmp[2:], 0)
	binary.BigEndian.PutUint16(icmp[2:], icmpChecksum(icmp))
	ok, gotSrc, it, ic := validateEchoReply(rep, id, seq, nonce)
	require.True(t, ok)
	require.True(t, gotSrc.Equal(src))
	require.Equal(t, 0, it)
	require.Equal(t, 0, ic)
	ok, _, _, _ = validateEchoReply(rep, id, seq, nonce+1)
	require.False(t, ok)
}

// Verifies that a basic ping to localhost succeeds using loopback interface.
func TestUping_Sender_Localhost_Success(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	s, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer s.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	res, err := s.Send(ctx, SendConfig{Target: net.IPv4(127, 0, 0, 1), Count: 2, Timeout: 800 * time.Millisecond})
	require.NoError(t, err)
	require.Len(t, res.Results, 2)
	for i, r := range res.Results {
		require.NoErrorf(t, r.Error, "i=%d", i)
		require.Greaterf(t, r.RTT, time.Duration(0), "i=%d", i)
		require.LessOrEqualf(t, r.RTT, time.Second, "i=%d", i)
	}
}

// Confirms packets are correctly steered through a specific interface (loopback).
func TestUping_Sender_Interface_Steer_Loopback(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	s, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer s.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	res, err := s.Send(ctx, SendConfig{Target: net.IPv4(127, 0, 0, 1), Count: 1, Timeout: 800 * time.Millisecond})
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	require.NoError(t, res.Results[0].Error)
	require.Greater(t, res.Results[0].RTT, time.Duration(0))
}

// Ensures timeout behavior when sending to a nonresponsive (blackhole) address.
func TestUping_Sender_Timeout_Blackhole(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	ip := pickLocalV4(t)
	ifname := ifaceNameForIP(t, ip)

	s, err := NewSender(SenderConfig{Source: ip, Interface: ifname})
	require.NoError(t, err)
	defer s.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 900*time.Millisecond)
	defer cancel()
	res, err := s.Send(ctx, SendConfig{Target: net.IPv4(203, 0, 113, 123), Count: 1, Timeout: 600 * time.Millisecond})
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	require.Error(t, res.Results[0].Error)
}

// Tests SendConfig validation logic for defaults and invalid parameters.
func TestUping_SendConfig_Validate_DefaultsAndErrors(t *testing.T) {
	t.Parallel()
	c := SendConfig{}
	err := c.Validate()
	require.NoError(t, err)
	require.Equal(t, defaultSenderCount, c.Count)
	require.Equal(t, defaultSenderTimeout, c.Timeout)
	require.Error(t, (&SendConfig{Count: -1, Timeout: time.Second}).Validate())
	require.Error(t, (&SendConfig{Count: 1, Timeout: -time.Second}).Validate())
}

// Rejects invalid IPv6 sources when creating a sender.
func TestUping_Sender_NewSender_InvalidSource(t *testing.T) {
	t.Parallel()
	_, err := NewSender(SenderConfig{Source: net.IPv6loopback, Interface: "lo"})
	require.Error(t, err)
}

// Rejects creation with nonexistent network interface.
func TestUping_Sender_NewSender_BadInterfaceName(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	_, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "does-not-exist-xyz"})
	require.Error(t, err)
}

// Ensures Send() exits cleanly if the context is canceled before sending.
func TestUping_Sender_ContextCanceledEarly(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	s, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer s.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	res, err := s.Send(ctx, SendConfig{Target: net.IPv4(127, 0, 0, 1), Count: 3, Timeout: 200 * time.Millisecond})
	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, res)
	require.Len(t, res.Results, 0)
}

// Validates that malformed or non-ICMP packets are rejected by the parser.
func TestUping_ValidateEchoReply_Negatives(t *testing.T) {
	t.Parallel()
	id, seq, nonce := uint16(1), uint16(2), uint64(3)

	ip := make([]byte, 20+16)
	ip[0] = 0x45
	ip[9] = 6
	copy(ip[12:16], net.IPv4(1, 2, 3, 4).To4())
	copy(ip[16:20], net.IPv4(5, 6, 7, 8).To4())
	binary.BigEndian.PutUint16(ip[10:], icmpChecksum(ip[:20]))
	ok, _, _, _ := validateEchoReply(ip, id, seq, nonce)
	require.False(t, ok)

	ok, _, _, _ = validateEchoReply([]byte{0x45, 0x00}, id, seq, nonce)
	require.False(t, ok)

	icmp := make([]byte, 8)
	icmp[0] = 3
	binary.BigEndian.PutUint16(icmp[2:], icmpChecksum(icmp))
	pkt := buildIPv4Packet(net.IPv4(9, 9, 9, 9), net.IPv4(1, 1, 1, 1), 1, icmp)
	ok, _, it, _ := validateEchoReply(pkt, id, seq, nonce)
	require.False(t, ok)
	require.Equal(t, 3, it)
}

// Confirms partial timeouts still return full count of results.
func TestUping_Sender_PartialTimeoutsStillReturnCount(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	sLo, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer sLo.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	okRes, err := sLo.Send(ctx, SendConfig{Target: net.IPv4(127, 0, 0, 1), Count: 1, Timeout: 500 * time.Millisecond})
	require.NoError(t, err)
	require.Len(t, okRes.Results, 1)
	require.NoError(t, okRes.Results[0].Error)

	ip := pickLocalV4(t)
	ifname := ifaceNameForIP(t, ip)
	sWAN, err := NewSender(SenderConfig{Source: ip, Interface: ifname})
	require.NoError(t, err)
	defer sWAN.Close()

	toRes, err := sWAN.Send(ctx, SendConfig{Target: net.IPv4(203, 0, 113, 123), Count: 1, Timeout: 400 * time.Millisecond})
	require.NoError(t, err)
	require.Len(t, toRes.Results, 1)
	require.Error(t, toRes.Results[0].Error)
}

// Checks SendResults.Failed() correctly identifies failures.
func TestUping_SendResults_Failed(t *testing.T) {
	t.Parallel()

	rs := &SendResults{Results: []SendResult{
		{RTT: 10 * time.Millisecond, Error: nil},
		{RTT: -1, Error: errors.New("timeout")},
	}}
	require.True(t, rs.Failed())

	rs2 := &SendResults{Results: []SendResult{
		{RTT: 1 * time.Millisecond, Error: nil},
	}}
	require.False(t, rs2.Failed())
}

// Rejects ICMP echo requests (type 8) as valid replies.
func TestUping_ValidateEchoReply_RejectsEchoRequest(t *testing.T) {
	t.Parallel()

	src := net.IPv4(10, 0, 0, 1).To4()
	dst := net.IPv4(10, 0, 0, 2).To4()
	id, seq, nonce := uint16(11), uint16(22), uint64(33)

	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, nonce)
	req := icmpEcho(id, seq, payload)
	ip := make([]byte, 20+len(req))
	ip[0] = 0x45
	ip[9] = 1
	copy(ip[12:16], src)
	copy(ip[16:20], dst)
	binary.BigEndian.PutUint16(ip[10:], icmpChecksum(ip[:20]))
	copy(ip[20:], req)

	ok, _, it, _ := validateEchoReply(ip, id, seq, nonce)
	require.False(t, ok)
	require.Equal(t, 8, it)
}

// Ensures socket reopen on send failure works and resumes successfully (PacketConn path).
func TestUping_Sender_ReopenOnSend_ReconnectAndSend(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	sIface, err := NewSender(SenderConfig{Source: net.IPv4(127, 0, 0, 1), Interface: "lo"})
	require.NoError(t, err)
	defer sIface.Close()

	s := sIface.(*sender)

	// Force a closed connection, then explicit reopen, then send should work.
	_ = s.ip4c.Close()
	_ = s.ipc.Close()
	require.NoError(t, s.reopen())

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	res, err := s.Send(ctx, SendConfig{Target: net.IPv4(127, 0, 0, 1), Count: 1, Timeout: 800 * time.Millisecond})
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	require.NoError(t, res.Results[0].Error)
	require.Greater(t, res.Results[0].RTT, time.Duration(0))
}

// Verifies recv path handles blackholed targets cleanly without races or crashes.
func TestUping_Sender_RecvTimeout_Blackhole_NoCrash(t *testing.T) {
	t.Parallel()
	requireRawSockets(t)

	ip := pickLocalV4(t)
	ifname := ifaceNameForIP(t, ip)

	sIface, err := NewSender(SenderConfig{Source: ip, Interface: ifname})
	require.NoError(t, err)
	defer sIface.Close()

	// Long enough overall context to let one probe time out cleanly on recv path.
	ctx, cancel := context.WithTimeout(t.Context(), 1200*time.Millisecond)
	defer cancel()

	// Blackhole target; expect probe-level timeout result, not a crash or top-level error.
	res, err := sIface.Send(ctx, SendConfig{
		Target:  net.IPv4(203, 0, 113, 200), // TEST-NET-3
		Count:   1,
		Timeout: 900 * time.Millisecond,
	})
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	require.Error(t, res.Results[0].Error)
}

// Verifies transientSocketErr correctly classifies recoverable errors.
func TestUping_Sender_TransientSocketErr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want bool
	}{
		{unix.EBADF, true},
		{unix.ENETDOWN, true},
		{unix.ENODEV, true},
		{unix.EADDRNOTAVAIL, true},
		{unix.ENOBUFS, true},
		{unix.ENETRESET, true},
		{unix.ENOMEM, true},
		{unix.EPERM, false},
		{unix.EINVAL, false},
		{fmt.Errorf("wrap: %w", unix.EBADF), true},
		{fmt.Errorf("wrap: %w", unix.ENOBUFS), true},
		{nil, false},
		{unix.EAGAIN, false},
		{errors.New("other"), false},
	}
	for i, tc := range cases {
		got := transientSocketErr(tc.err)
		if got != tc.want {
			t.Fatalf("case %d: err=%v got=%v want=%v", i, tc.err, got, tc.want)
		}
	}
}

// Verifies transientSendRetryable correctly classifies retryable errors.
func TestUping_Sender_TransientSendRetryable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want bool
	}{
		{unix.EBADF, true},
		{unix.ENODEV, true},
		{unix.ENETDOWN, true},
		{fmt.Errorf("wrap: %w", unix.EBADF), true},
		{fmt.Errorf("wrap: %w", unix.ENETDOWN), true},
		{nil, false},
		{unix.ENOBUFS, false},
		{unix.EADDRNOTAVAIL, false},
		{unix.ENETRESET, false},
		{unix.ENOMEM, false},
		{unix.EAGAIN, false},
	}
	for i, tc := range cases {
		got := transientSendRetryable(tc.err)
		if got != tc.want {
			t.Fatalf("case %d: err=%v got=%v want=%v", i, tc.err, got, tc.want)
		}
	}
}

// helper to build a minimal IPv4+ICMP frame
func buildIPv4Packet(src, dst net.IP, proto byte, payload []byte) []byte {
	ip := make([]byte, 20+len(payload))
	ip[0] = 0x45
	ip[9] = proto
	copy(ip[12:16], src.To4())
	copy(ip[16:20], dst.To4())
	binary.BigEndian.PutUint16(ip[10:], icmpChecksum(ip[:20]))
	copy(ip[20:], payload)
	return ip
}

// Require CAP_NET_RAW by attempting to bind an ICMP socket. Skips/errs like before.
func requireRawSockets(t *testing.T) {
	c, err := net.ListenIP("ip4:icmp", &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err == nil {
		_ = c.Close()
		return
	}
	require.NoError(t, err)
}

// Pick a non-loopback IPv4 if available, else fall back to loopback.
func pickLocalV4(t *testing.T) net.IP {
	ifs, err := net.Interfaces()
	require.NoError(t, err)
	for _, ifi := range ifs {
		if (ifi.Flags & net.FlagUp) == 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
				return ipn.IP.To4()
			}
		}
	}
	return net.IPv4(127, 0, 0, 1)
}

// find the interface name that owns the given IPv4 address (exact match preferred,
// falls back to subnet containment). Fails the test if not found.
func ifaceNameForIP(t *testing.T, ip net.IP) string {
	ifs, err := net.Interfaces()
	require.NoError(t, err)
	for _, ifi := range ifs {
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				if ipn.IP.To4().Equal(ip.To4()) {
					return ifi.Name
				}
			}
		}
	}
	for _, ifi := range ifs {
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				if ipn.Contains(ip) {
					return ifi.Name
				}
			}
		}
	}
	t.Fatalf("could not find interface name for ip %v", ip)
	return ""
}

func icmpEcho(id, seq uint16, payload []byte) []byte {
	h := make([]byte, 8+len(payload))
	h[0] = 8
	binary.BigEndian.PutUint16(h[4:], id)
	binary.BigEndian.PutUint16(h[6:], seq)
	copy(h[8:], payload)
	binary.BigEndian.PutUint16(h[2:], icmpChecksum(h))
	return h
}
