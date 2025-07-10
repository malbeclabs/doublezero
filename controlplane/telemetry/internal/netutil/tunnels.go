package netutil

import (
	"fmt"
	"net"
)

type LocalTunnel struct {
	Interface string
	SourceIP  net.IP
	TargetIP  net.IP
}

func FindLocalTunnel(interfaces []Interface, tunnelNet *net.IPNet) (*LocalTunnel, error) {
	for _, iface := range interfaces {
		for _, addr := range iface.Addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			// Check that it's a valid IPv4 address.
			ip := ipNet.IP.To4()
			if ip == nil {
				continue
			}

			// Check that it's a /31 interface.
			if ones, bits := ipNet.Mask.Size(); ones != 31 || bits != 32 {
				continue
			}

			// Check that the interface IP is in the tunnel net.
			if tunnelNet.Contains(ip) {
				peerIP, err := getPeerIPIn31(ip.String())
				if err != nil {
					return nil, fmt.Errorf("failed to get peer ip in /31: %w", err)
				}

				peerIP = peerIP.To4()
				if peerIP == nil {
					return nil, fmt.Errorf("not an IPv4 address: %s", peerIP.String())
				}

				return &LocalTunnel{
					Interface: iface.Name,
					SourceIP:  ip,
					TargetIP:  peerIP,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no local tunnel found for subnet %s", tunnelNet)
}

func getPeerIPIn31(ipStr string) (net.IP, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP: %s", ipStr)
	}
	ip = ip.To4()
	if ip == nil {
		return nil, fmt.Errorf("not an IPv4 address: %s", ipStr)
	}
	ipInt := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	peerInt := ipInt ^ 1
	peerIP := net.IPv4(byte(peerInt>>24), byte(peerInt>>16), byte(peerInt>>8), byte(peerInt))
	return peerIP, nil
}
