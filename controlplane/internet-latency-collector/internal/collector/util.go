package collector

import (
	"net"
)

const InvalidDistanceKM = 9999.0

const TimeFormatMicroseconds = "2006-01-02T15:04:05.000000"

func IsInternetRoutable(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}

	privateIPv4Blocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
	}

	for _, cidr := range privateIPv4Blocks {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return false
		}
	}

	if ip.To4() == nil {
		if len(ip) >= 1 && (ip[0]&0xfe) == 0xfc {
			return false
		}
	}

	return true
}
