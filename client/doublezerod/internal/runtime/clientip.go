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
//  2. Scan local network interfaces for a publicly routable IPv4 address.
//  3. Fall back to querying https://ifconfig.me/ip (5s timeout).
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

	// 2. Scan local interfaces.
	if ip, err := discoverFromInterfaces(); err == nil {
		return ip, "local interface", nil
	}

	// 3. External discovery.
	ip, err := discoverFromExternal()
	if err != nil {
		return nil, "", fmt.Errorf("client IP discovery failed: %w", err)
	}
	return ip, "external discovery (ifconfig.me)", nil
}

// discoverFromInterfaces scans local network interfaces for a publicly
// routable IPv4 address. On multi-homed hosts the first public IP found
// is returned; use the explicit --client-ip flag if a specific interface
// is required.
func discoverFromInterfaces() (net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			slog.Debug("client-ip: error reading addrs from interface", "iface", iface.Name, "error", err)
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if IsPublicIPv4(ip) {
				return ip.To4(), nil
			}
		}
	}
	return nil, fmt.Errorf("no public IPv4 address found on local interfaces")
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
