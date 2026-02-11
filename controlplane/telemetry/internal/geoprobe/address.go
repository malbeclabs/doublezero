package geoprobe

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

var (
	// RFC 1918 private IPv4 ranges
	private10  = mustParseCIDR("10.0.0.0/8")
	private172 = mustParseCIDR("172.16.0.0/12")
	private192 = mustParseCIDR("192.168.0.0/16")

	// RFC 3330 special-use IPv4 ranges
	loopback     = mustParseCIDR("127.0.0.0/8")
	linkLocal    = mustParseCIDR("169.254.0.0/16")
	zeroNet      = mustParseCIDR("0.0.0.0/8")
	reserved240  = mustParseCIDR("240.0.0.0/4")
	broadcast    = mustParseCIDR("255.255.255.255/32")
	multicast224 = mustParseCIDR("224.0.0.0/4")

	// RFC 4193 private IPv6 range
	privateIPv6FC = mustParseCIDR("fc00::/7")
	// RFC 4291 IPv6 link-local
	linkLocalIPv6 = mustParseCIDR("fe80::/10")
)

func mustParseCIDR(s string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		panic(fmt.Sprintf("invalid CIDR %q: %v", s, err))
	}
	return ipnet
}

// isPrivateIP checks if the IP address is in a private range.
func isPrivateIP(ip net.IP) bool {
	if ip.To4() != nil {
		return private10.Contains(ip) ||
			private172.Contains(ip) ||
			private192.Contains(ip)
	}
	return privateIPv6FC.Contains(ip)
}

// isLoopback checks if the IP address is a loopback address.
func isLoopback(ip net.IP) bool {
	if ip.To4() != nil {
		return loopback.Contains(ip)
	}
	return ip.IsLoopback()
}

// isLinkLocal checks if the IP address is link-local.
func isLinkLocal(ip net.IP) bool {
	if ip.To4() != nil {
		return linkLocal.Contains(ip)
	}
	return linkLocalIPv6.Contains(ip)
}

// isReservedIP checks if the IP address is in a reserved range.
func isReservedIP(ip net.IP) bool {
	if ip.To4() != nil {
		return zeroNet.Contains(ip) ||
			reserved240.Contains(ip) ||
			broadcast.Contains(ip) ||
			multicast224.Contains(ip)
	}
	return false
}

// isPublicIP checks if the IP address is a public (routable) address.
func isPublicIP(ip net.IP) bool {
	return !isPrivateIP(ip) &&
		!isLoopback(ip) &&
		!isLinkLocal(ip) &&
		!isReservedIP(ip) &&
		!ip.IsUnspecified()
}

// ProbeAddress represents a child geoProbe's network address.
type ProbeAddress struct {
	Host string
	Port uint16
}

func (p ProbeAddress) String() string {
	return fmt.Sprintf("%s:%d", p.Host, p.Port)
}

// Validate checks if the ProbeAddress has valid Host and Port values.
func (p ProbeAddress) Validate() error {
	if p.Host == "" {
		return fmt.Errorf("host cannot be empty")
	}
	if p.Port == 0 {
		return fmt.Errorf("port cannot be zero")
	}
	return nil
}

// ValidatePublic checks if the ProbeAddress resolves to a public IP address.
func (p ProbeAddress) ValidatePublic(ctx context.Context) error {
	if err := p.Validate(); err != nil {
		return err
	}

	resolver := &net.Resolver{}
	ips, err := resolver.LookupHost(ctx, p.Host)
	if err != nil {
		return fmt.Errorf("failed to resolve host %s: %w", p.Host, err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("no IP addresses found for host %s", p.Host)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}

		if !isPublicIP(ip) {
			if isPrivateIP(ip) {
				return fmt.Errorf("host %s resolves to private IP address %s", p.Host, ip)
			}
			if isLoopback(ip) {
				return fmt.Errorf("host %s resolves to loopback IP address %s", p.Host, ip)
			}
			if isLinkLocal(ip) {
				return fmt.Errorf("host %s resolves to link-local IP address %s", p.Host, ip)
			}
			if isReservedIP(ip) {
				return fmt.Errorf("host %s resolves to reserved IP address %s", p.Host, ip)
			}
			if ip.IsUnspecified() {
				return fmt.Errorf("host %s resolves to unspecified IP address %s", p.Host, ip)
			}
			return fmt.Errorf("host %s resolves to non-public IP address %s", p.Host, ip)
		}
	}

	return nil
}

// ParseProbeAddresses parses a comma-separated list of host:port values.
func ParseProbeAddresses(s string) ([]ProbeAddress, error) {
	return ParseProbeAddressesWithContext(context.Background(), s)
}

// ParseProbeAddressesWithContext parses a comma-separated list of host:port values
// and validates that each address resolves to a public IP.
func ParseProbeAddressesWithContext(ctx context.Context, s string) ([]ProbeAddress, error) {
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	probes := make([]ProbeAddress, 0, len(parts))
	seen := make(map[string]bool)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		host, portStr, err := net.SplitHostPort(part)
		if err != nil {
			return nil, fmt.Errorf("invalid probe address %q: %w", part, err)
		}

		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid port in %q: %w", part, err)
		}
		if port == 0 {
			return nil, fmt.Errorf("invalid port 0 in %q", part)
		}

		addr := ProbeAddress{Host: host, Port: uint16(port)}

		if err := addr.ValidatePublic(ctx); err != nil {
			return nil, fmt.Errorf("invalid probe address %q: %w", part, err)
		}

		// Deduplicate
		key := addr.String()
		if seen[key] {
			continue
		}
		seen[key] = true

		probes = append(probes, addr)
	}

	return probes, nil
}

// resolveUDPAddrWithTimeout resolves a UDP address with a timeout to prevent indefinite blocking.
// It uses a custom resolver with the provided timeout context.
func resolveUDPAddrWithTimeout(ctx context.Context, address string, timeout time.Duration) (*net.UDPAddr, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address format: %w", err)
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	resolveCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resolver := &net.Resolver{}
	ips, err := resolver.LookupHost(resolveCtx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve host %s: %w", host, err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found for host %s", host)
	}

	ip := net.ParseIP(ips[0])
	if ip == nil {
		return nil, fmt.Errorf("failed to parse IP address %s", ips[0])
	}

	return &net.UDPAddr{
		IP:   ip,
		Port: int(port),
	}, nil
}
