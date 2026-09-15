package manager

import (
	"fmt"
	"log/slog"
	"net"
)

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
