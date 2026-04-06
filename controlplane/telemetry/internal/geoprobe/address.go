package geoprobe

import (
	"fmt"
	"net"
)

// Non-routable IPv4 ranges that net.IP.IsPrivate() and IsGlobalUnicast() miss.
var additionalNonPublicIPv4Ranges = []net.IPNet{
	{IP: net.IP{100, 64, 0, 0}, Mask: net.CIDRMask(10, 32)},   // CGN / shared address space (RFC 6598)
	{IP: net.IP{198, 18, 0, 0}, Mask: net.CIDRMask(15, 32)},   // Benchmarking (RFC 2544)
	{IP: net.IP{192, 0, 2, 0}, Mask: net.CIDRMask(24, 32)},    // TEST-NET-1 (RFC 5737)
	{IP: net.IP{198, 51, 100, 0}, Mask: net.CIDRMask(24, 32)}, // TEST-NET-2 (RFC 5737)
	{IP: net.IP{203, 0, 113, 0}, Mask: net.CIDRMask(24, 32)},  // TEST-NET-3 (RFC 5737)
}

// ProbeAddress represents a child geoProbe's network address.
type ProbeAddress struct {
	Host      string
	Port      uint16 // UDP offset port (used by Publisher)
	TWAMPPort uint16 // TWAMP reflector port (used by Pinger)
}

func (p ProbeAddress) String() string {
	return fmt.Sprintf("%s:%d:%d", p.Host, p.Port, p.TWAMPPort)
}

// Validate checks if the ProbeAddress has valid Host, Port, and TWAMPPort values.
func (p ProbeAddress) Validate() error {
	if err := p.ValidateICMP(); err != nil {
		return err
	}
	if p.TWAMPPort == 0 {
		return fmt.Errorf("twamp port cannot be zero")
	}
	return nil
}

// ValidateICMP checks if the ProbeAddress is valid for ICMP probing.
// Unlike Validate(), it does not require TWAMPPort to be set.
func (p ProbeAddress) ValidateICMP() error {
	if p.Host == "" {
		return fmt.Errorf("host cannot be empty")
	}
	ip := net.ParseIP(p.Host)
	if ip == nil {
		return fmt.Errorf("host must be a valid IP address")
	}
	if ip.To4() == nil {
		return fmt.Errorf("host %s must be an IPv4 address", p.Host)
	}
	if p.Port == 0 {
		return fmt.Errorf("port cannot be zero")
	}
	return nil
}

func isNonPublicUnicast(ip net.IP) bool {
	for _, block := range additionalNonPublicIPv4Ranges {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateScope rejects non-public unicast addresses (loopback, private, link-local,
// multicast, unspecified). Use this for addresses sourced from untrusted onchain data
// to prevent SSRF-like attacks against internal networks.
func (p ProbeAddress) ValidateScope() error {
	ip := net.ParseIP(p.Host)
	if ip == nil {
		return fmt.Errorf("host must be a valid IP address")
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || isNonPublicUnicast(ip) {
		return fmt.Errorf("host %s is not a public unicast address", p.Host)
	}
	return nil
}
