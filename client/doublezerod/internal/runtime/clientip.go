package runtime

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

const externalDiscoveryTimeout = 5 * time.Second

var (
	// Private/reserved IPv4 ranges that are not publicly routable.
	privateRanges []*net.IPNet
)

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"100.64.0.0/10",  // CGNAT (RFC6598)
		"127.0.0.0/8",    // Loopback
		"169.254.0.0/16", // Link-local
		"224.0.0.0/4",    // Multicast
		"240.0.0.0/4",    // Reserved
		"0.0.0.0/8",      // "This" network
		"255.255.255.255/32",
	} {
		_, ipNet, _ := net.ParseCIDR(cidr)
		privateRanges = append(privateRanges, ipNet)
	}
}

// IsPublicIPv4 reports whether ip is a publicly routable IPv4 address.
// It returns false for loopback, link-local, RFC1918, CGNAT, multicast,
// and other reserved ranges.
func IsPublicIPv4(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}
	for _, r := range privateRanges {
		if r.Contains(ip) {
			return false
		}
	}
	return true
}

// DiscoverClientIP determines the client's public IP address.
//
// Resolution order:
//  1. If explicit is non-empty, parse and return it.
//  2. Ask the kernel for the default route's source address (via a UDP
//     dial to 8.8.8.8 — no packets are sent). If the source is a publicly
//     routable IPv4 address, use it.
//  3. Fall back to querying https://ifconfig.me/ip (5s timeout).
//
// Step 2 replaces the previous approach of scanning all network interfaces
// for the first public IP, which was unsafe on multi-homed hosts: it could
// pick an address on an interface that doesn't match the default route,
// causing a mismatch with the onchain User's ClientIp.
//
// The second return value describes the discovery method for logging.
func DiscoverClientIP(explicit string) (net.IP, string, error) {
	// 1. Explicit flag.
	if explicit != "" {
		ip := net.ParseIP(explicit)
		if ip == nil {
			return nil, "", fmt.Errorf("invalid client-ip flag value: %s", explicit)
		}
		ip = ip.To4()
		if ip == nil {
			return nil, "", fmt.Errorf("client-ip must be IPv4: %s", explicit)
		}
		return ip, "explicit flag", nil
	}

	// 2. Default route source hint.
	if ip, err := discoverFromDefaultRoute(); err == nil {
		return ip, "default route", nil
	} else {
		slog.Debug("client-ip: default route discovery failed, falling back to external", "error", err)
	}

	// 3. External discovery.
	ip, err := discoverFromExternal()
	if err != nil {
		return nil, "", fmt.Errorf("client IP discovery failed: %w", err)
	}
	return ip, "external discovery (ifconfig.me)", nil
}

// discoverFromDefaultRoute performs a kernel route lookup by dialing a
// well-known public IP over UDP (no packets are actually sent). The local
// address chosen by the kernel reflects the default route's source hint,
// which is the address that outbound traffic would actually use.
func discoverFromDefaultRoute() (net.IP, error) {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return nil, fmt.Errorf("route lookup failed: %w", err)
	}
	defer conn.Close()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, fmt.Errorf("unexpected local address type: %T", conn.LocalAddr())
	}

	ip := localAddr.IP.To4()
	if ip == nil {
		return nil, fmt.Errorf("default route source is not IPv4: %v", localAddr.IP)
	}
	if !IsPublicIPv4(ip) {
		return nil, fmt.Errorf("default route source %s is not publicly routable", ip)
	}
	return ip, nil
}

func discoverFromExternal() (net.IP, error) {
	client := &http.Client{Timeout: externalDiscoveryTimeout}
	resp, err := client.Get("https://ifconfig.me/ip")
	if err != nil {
		return nil, fmt.Errorf("HTTP request to ifconfig.me failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ifconfig.me returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return nil, fmt.Errorf("reading ifconfig.me response: %w", err)
	}

	ip := net.ParseIP(strings.TrimSpace(string(body)))
	if ip == nil {
		return nil, fmt.Errorf("ifconfig.me returned invalid IP: %q", string(body))
	}
	ip = ip.To4()
	if ip == nil {
		return nil, fmt.Errorf("ifconfig.me returned non-IPv4 address (only IPv4 is supported): %s", ip)
	}
	return ip, nil
}
