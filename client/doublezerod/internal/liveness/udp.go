package liveness

import (
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/ipv4"
)

const ifCacheTTL = 30 * time.Second

type UDPService interface {
	ReadFrom(buf []byte) (n int, remoteAddr *net.UDPAddr, localIP net.IP, ifname string, err error)
	WriteTo(pkt []byte, dst *net.UDPAddr, iface string, src net.IP) (int, error)
	SetReadDeadline(t time.Time) error
	LocalAddr() net.Addr
	Close() error
}

// UDPService wraps an IPv4 UDP socket and provides helpers for reading and writing
// datagrams while preserving local interface and destination address context.
// It preconfigures IPv4 control message delivery (IP_PKTINFO equivalent) so that
// each received packet includes metadata about which interface and destination IP
// it arrived on, and outgoing packets can explicitly set source IP and interface.
type udpService struct {
	raw     *net.UDPConn     // the underlying UDP socket
	pc4     *ipv4.PacketConn // ipv4-layer wrapper for control message access
	ifcache *ifCache         // cached interface index<->name mappings
}

// ListenUDP binds an IPv4 UDP socket to bindIP:port and returns a configured UDPService.
// The returned connection is ready to read/write with control message support enabled.
func ListenUDP(bindIP string, port int) (*udpService, error) {
	laddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", bindIP, port))
	if err != nil {
		return nil, err
	}
	raw, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return nil, err
	}
	u, err := NewUDPService(raw)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return u, nil
}

// NewUDPService wraps an existing *net.UDPConn and enables IPv4 control messages (IP_PKTINFO-like).
// On RX we obtain the destination IP and interface index; on TX we can set source IP and interface.
func NewUDPService(raw *net.UDPConn) (*udpService, error) {
	u := &udpService{raw: raw, pc4: ipv4.NewPacketConn(raw), ifcache: newIfCache(ifCacheTTL)}
	// Enable both RX and TX control messages: destination IP, source IP, and interface index.
	if err := u.pc4.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst|ipv4.FlagSrc, true); err != nil {
		return nil, err
	}
	return u, nil
}

// Close shuts down the underlying UDP socket.
func (u *udpService) Close() error { return u.raw.Close() }

// ReadFrom reads a single UDP datagram and returns:
//   - number of bytes read
//   - remoteAddr address (sender)
//   - local destination IP the packet was received on
//   - interface name where it arrived
//
// The caller should configure read deadlines via SetReadDeadline before calling.
// This function extracts control message metadata (IP_PKTINFO) to provide per-packet context.
func (u *udpService) ReadFrom(buf []byte) (n int, remoteAddr *net.UDPAddr, localIP net.IP, ifname string, err error) {
	n, cm4, raddr, err := u.pc4.ReadFrom(buf)
	if err != nil {
		return 0, nil, nil, "", err
	}
	if ua, ok := raddr.(*net.UDPAddr); ok {
		remoteAddr = ua
	}
	if cm4 != nil {
		if cm4.Dst != nil {
			localIP = cm4.Dst
		}
		if cm4.IfIndex != 0 {
			ifname = u.ifcache.NameByIndex(cm4.IfIndex)
		}
	}
	return n, remoteAddr, localIP, ifname, nil
}

// WriteTo transmits a UDP datagram to an IPv4 destination.
// The caller may optionally provide:
//   - iface: name of the outgoing interface to bind transmission to
//   - src:   source IP to use (if nil, the kernel selects one)
//
// Returns number of bytes written or an error.
// This uses an ipv4.ControlMessage to set per-packet src/interface hints.
func (u *udpService) WriteTo(pkt []byte, dst *net.UDPAddr, iface string, src net.IP) (int, error) {
	if dst == nil || dst.IP == nil {
		return 0, errors.New("nil dst")
	}
	ip4 := dst.IP.To4()
	if ip4 == nil {
		return 0, errors.New("ipv6 dst not supported")
	}

	var ifidx int
	if iface != "" {
		idx, ok := u.ifcache.IndexByName(iface)
		if !ok {
			return 0, fmt.Errorf("interface %q not found", iface)
		}
		ifidx = idx
	}

	var cm ipv4.ControlMessage
	if ifidx != 0 {
		cm.IfIndex = ifidx
	}
	if src != nil {
		if s4 := src.To4(); s4 != nil {
			cm.Src = s4
		}
		// Non-IPv4 src ignored silently in IPv4 mode.
	}

	return u.pc4.WriteTo(pkt, &cm, &net.UDPAddr{IP: ip4, Port: dst.Port, Zone: dst.Zone})
}

// SetReadDeadline forwards directly to the underlying UDPService.
// This controls how long ReadFrom will block before returning a timeout.
func (u *udpService) SetReadDeadline(t time.Time) error {
	return u.raw.SetReadDeadline(t)
}

// LocalAddr returns the socket’s bound local address (IP and port).
func (u *udpService) LocalAddr() net.Addr {
	return u.raw.LocalAddr()
}
