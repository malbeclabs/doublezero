package manager

import (
	"fmt"
	"log/slog"
	"net"
)

// bgpMartianNets contains the standard BGP martian prefixes — addresses that should never
// appear in BGP routing tables, and so never as a DoubleZero client IP.
var bgpMartianNets []*net.IPNet

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

// IsPublicIPv4 reports whether ip is a publicly routable IPv4 address. It returns false for any
// address in a standard BGP martian prefix (loopback, link-local, RFC 1918, CGNAT, documentation,
// multicast, and other reserved ranges).
//
// It lives here rather than beside discovery because both the discovered address and a pinned one
// have to satisfy it, and the pin arrives at this package first.
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

// isLocallyAssigned is indirected so tests can exercise the handler with an address this
// host does not hold. Production always uses IsLocallyAssigned.
var isLocallyAssigned = IsLocallyAssigned

// IsLocallyAssigned reports whether ip is assigned to an interface that is up and running
// on this host.
//
// A pinned client IP has to satisfy this before the daemon provisions against it: for plain
// IBRL the address is used verbatim as the GRE tunnel source, so one the kernel does not hold
// cannot carry a tunnel. Checking it here — rather than trusting the caller — is what keeps a
// malformed or malicious /enable from leaving the host with a half-built configuration.
//
// IFF_UP alone is only the administrative state; an interface can be up with no carrier.
// Both flags together are the kernel's notion of "usable right now".
func IsLocallyAssigned(ip net.IP) (bool, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false, fmt.Errorf("enumerating interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagRunning == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			// One unreadable interface must not hide an address held on another.
			slog.Debug("client-ip: skipping interface with unreadable addresses", "iface", iface.Name, "error", err)
			continue
		}
		for _, addr := range addrs {
			var candidate net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				candidate = v.IP
			case *net.IPAddr:
				candidate = v.IP
			default:
				continue
			}
			if candidate.To4() != nil && candidate.Equal(ip) {
				return true, nil
			}
		}
	}
	return false, nil
}
