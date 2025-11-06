package liveness

import (
	"errors"
	"net"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func readFromUDP(conn net.PacketConn, buf []byte) (n int, remoteAddr *net.UDPAddr, localIP net.IP, ifname string, err error) {
	p := ipv4.NewPacketConn(conn)
	if err = p.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true); err != nil {
		return
	}
	var cm *ipv4.ControlMessage
	var raddr net.Addr
	n, cm, raddr, err = p.ReadFrom(buf)
	if err != nil {
		return
	}
	if ua, ok := raddr.(*net.UDPAddr); ok {
		remoteAddr = ua
	}
	if cm != nil && cm.Dst != nil {
		localIP = cm.Dst
	}
	if cm != nil && cm.IfIndex != 0 {
		ifi, _ := net.InterfaceByIndex(cm.IfIndex)
		if ifi != nil {
			ifname = ifi.Name
		}
	}
	return
}

func writeUDP(conn net.PacketConn, pkt []byte, dst *net.UDPAddr, iface string, src net.IP) (int, error) {
	if dst == nil || dst.IP == nil {
		return 0, errors.New("nil dst")
	}
	var ifidx int
	if iface != "" {
		ifi, err := net.InterfaceByName(iface)
		if err != nil {
			return 0, err
		}
		ifidx = ifi.Index
	}

	if ip4 := dst.IP.To4(); ip4 != nil {
		pc := ipv4.NewPacketConn(conn)
		if err := pc.SetControlMessage(ipv4.FlagInterface|ipv4.FlagSrc, true); err != nil {
			return 0, err
		}
		var cm ipv4.ControlMessage
		if ifidx != 0 {
			cm.IfIndex = ifidx
		}
		if src != nil {
			cm.Src = src
		}
		return pc.WriteTo(pkt, &cm, &net.UDPAddr{IP: ip4, Port: dst.Port, Zone: dst.Zone})
	}

	pc := ipv6.NewPacketConn(conn)
	if err := pc.SetControlMessage(ipv6.FlagInterface|ipv6.FlagSrc, true); err != nil {
		return 0, err
	}
	var cm ipv6.ControlMessage
	if ifidx != 0 {
		cm.IfIndex = ifidx
	}
	if src != nil {
		cm.Src = src
	}
	return pc.WriteTo(pkt, &cm, dst)
}
