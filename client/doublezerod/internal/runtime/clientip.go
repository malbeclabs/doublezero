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

const (
	externalDiscoveryTimeout = 5 * time.Second
	externalDiscoveryRetries = 3
)

var externalDiscoveryBackoff = 2 * time.Second

var (
	// bgpMartianNets contains the standard BGP martian prefixes — addresses
	// that should never appear in BGP routing tables.
	bgpMartianNets []*net.IPNet

	// externalDiscoveryURL is the endpoint queried for external IP discovery.
	// Overridden in tests.
	externalDiscoveryURL = "https://ifconfig.me/ip"
)

func init() {
	for _, cidr := range []string{
		"0.0.0.0/8",       // "this" network (RFC 1122)
		"10.0.0.0/8",      // private (RFC 1918)
		"100.64.0.0/10",   // shared address space / CGNAT (RFC 6598)
		"127.0.0.0/8",     // loopback (RFC 1122)
		"169.254.0.0/16",  // link-local (RFC 3927)
		"172.16.0.0/12",   // private (RFC 1918)
		"192.0.0.0/24",    // IETF protocol assignments (RFC 6890)
		"192.0.2.0/24",    // documentation TEST-NET-1 (RFC 5737)
		"192.168.0.0/16",  // private (RFC 1918)
		"198.51.100.0/24", // documentation TEST-NET-2 (RFC 5737)
		"203.0.113.0/24",  // documentation TEST-NET-3 (RFC 5737)
		"224.0.0.0/4",     // multicast (RFC 5771)
		"240.0.0.0/4",     // reserved (RFC 1112)
		"255.255.255.255/32",
	} {
		_, ipNet, _ := net.ParseCIDR(cidr)
		bgpMartianNets = append(bgpMartianNets, ipNet)
	}
}

// IsPublicIPv4 reports whether ip is a publicly routable IPv4 address.
// It returns false for any address that falls within a standard BGP martian
// prefix (loopback, link-local, RFC 1918, CGNAT, documentation, multicast,
// and other reserved ranges).
func IsPublicIPv4(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}
	for _, n := range bgpMartianNets {
		if n.Contains(ip) {
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
	var lastErr error
	for attempt := range externalDiscoveryRetries {
		if attempt > 0 {
			slog.Warn("client-ip: retrying external discovery", "attempt", attempt+1, "error", lastErr)
			time.Sleep(externalDiscoveryBackoff)
		}
		ip, err := queryExternalIP()
		if err == nil {
			return ip, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("external IP discovery failed after %d attempts: %w", externalDiscoveryRetries, lastErr)
}

func queryExternalIP() (net.IP, error) {
	client := &http.Client{Timeout: externalDiscoveryTimeout}
	resp, err := client.Get(externalDiscoveryURL)
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
