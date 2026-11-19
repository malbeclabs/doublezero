package allowlist

import (
	"net/netip"
)

// isGlobalIPv4 reports whether addr may go on the list: a global IPv4 address.
// It follows is_global in the serviceability program
// (smartcontract/programs/doublezero-serviceability/src/helper.rs), which is
// what the program enforces on agent_ip, so an address the program accepts is
// never dropped here.
func isGlobalIPv4(addr netip.Addr) bool {
	if !addr.Is4() {
		return false
	}
	if addr.IsUnspecified() || addr.IsPrivate() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsMulticast() {
		return false
	}
	for _, p := range nonGlobalIPv4 {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

var nonGlobalIPv4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this" network
	netip.MustParsePrefix("100.64.0.0/10"),   // shared address space (CGNAT)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, which holds broadcast
}
