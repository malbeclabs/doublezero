package liveness

import (
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/ipv4"
)

// UDPConn wraps a UDP socket (IPv4-only) and provides read/write with
// control messages configured once at construction time.
type UDPConn struct {
	raw *net.UDPConn
	pc4 *ipv4.PacketConn
}

// ListenUDP binds to bindIP:port using IPv4 and returns a configured UDPConn.
func ListenUDP(bindIP string, port int) (*UDPConn, error) {
	laddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", bindIP, port))
	if err != nil {
		return nil, err
	}
	raw, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return nil, err
	}
	u, err := NewUDPConn(raw)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return u, nil
}

// NewUDPConn wraps an existing *net.UDPConn and preconfigures IPv4 control messages.
func NewUDPConn(raw *net.UDPConn) (*UDPConn, error) {
	u := &UDPConn{raw: raw, pc4: ipv4.NewPacketConn(raw)}
	// Enable RX + TX control messages once (dst/src IP + interface index).
	if err := u.pc4.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst|ipv4.FlagSrc, true); err != nil {
		return nil, err
	}
	return u, nil
}

// Close closes the underlying socket.
func (u *UDPConn) Close() error { return u.raw.Close() }

// ReadFrom reads a packet and returns (n, remote, localIP=dst, ifname).
// Deadline should be set via SetReadDeadline on u.raw.
func (u *UDPConn) ReadFrom(buf []byte) (n int, remote *net.UDPAddr, localIP net.IP, ifname string, err error) {
	n, cm4, raddr, err := u.pc4.ReadFrom(buf)
	if err != nil {
		return 0, nil, nil, "", err
	}
	if ua, ok := raddr.(*net.UDPAddr); ok {
		remote = ua
	}
	if cm4 != nil {
		if cm4.Dst != nil {
			localIP = cm4.Dst
		}
		if cm4.IfIndex != 0 {
			ifi, _ := net.InterfaceByIndex(cm4.IfIndex)
			if ifi != nil {
				ifname = ifi.Name
			}
		}
	}
	return n, remote, localIP, ifname, nil
}

// WriteTo sends pkt to dst, optionally pinning the outgoing interface and source IP.
// Only IPv4 destinations are supported.
func (u *UDPConn) WriteTo(pkt []byte, dst *net.UDPAddr, iface string, src net.IP) (int, error) {
	if dst == nil || dst.IP == nil {
		return 0, errors.New("nil dst")
	}
	// Require IPv4 destination.
	ip4 := dst.IP.To4()
	if ip4 == nil {
		return 0, errors.New("ipv6 dst not supported")
	}

	var ifidx int
	if iface != "" {
		ifi, err := net.InterfaceByName(iface)
		if err != nil {
			return 0, err
		}
		ifidx = ifi.Index
	}

	var cm ipv4.ControlMessage
	if ifidx != 0 {
		cm.IfIndex = ifidx
	}
	if src != nil {
		if s4 := src.To4(); s4 != nil {
			cm.Src = s4
		} else {
			// ignore non-IPv4 src hints in IPv4 mode
		}
	}
	return u.pc4.WriteTo(pkt, &cm, &net.UDPAddr{IP: ip4, Port: dst.Port, Zone: dst.Zone})
}

// SetReadDeadline forwards to the underlying socket.
func (u *UDPConn) SetReadDeadline(t time.Time) error { return u.raw.SetReadDeadline(t) }

// LocalAddr returns the underlying socket's local address.
func (u *UDPConn) LocalAddr() net.Addr { return u.raw.LocalAddr() }
