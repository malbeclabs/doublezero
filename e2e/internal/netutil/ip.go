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
