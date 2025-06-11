package netutil

import (
	"fmt"
	"net"
)

func BuildIPInCIDR(cidr string, lastOctet byte) (net.IP, error) {
	parsedIP, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CYOA network subnet: %w", err)
	}

	ip4 := parsedIP.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("failed to parse CYOA network subnet as IPv4")
	}

	ip4[3] = lastOctet

	return ip4, nil
}
