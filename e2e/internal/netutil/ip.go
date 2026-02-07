package netutil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

func DeriveIPFromCIDR(cidr string, hostID uint32) (net.IP, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}

	ip = ip.To4()
	if ip == nil {
		return nil, errors.New("only IPv4 is supported")
	}

	prefixLen, bits := ipnet.Mask.Size()
	hostBits := bits - prefixLen
	if hostID >= (1 << hostBits) {
		return nil, fmt.Errorf("host ID %d out of range for /%d", hostID, prefixLen)
	}

	ipInt := binary.BigEndian.Uint32(ip)
	ipInt += hostID

	result := make(net.IP, 4)
	binary.BigEndian.PutUint32(result, ipInt)
	return result, nil
}

// ParseCIDR parses a CIDR string and returns the IP and network.
func ParseCIDR(cidr string) (string, *net.IPNet, error) {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", nil, err
	}
	return ip.String(), network, nil
}

// IPInRange checks if an IP is within a CIDR block.
func IPInRange(ipStr, cidr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return network.Contains(ip)
}
