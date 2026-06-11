package geolocation

import "fmt"

// validatePublicIP mirrors doublezero-geolocation/src/validation.rs::validate_public_ip.
// It rejects any IPv4 address that is not globally routable: RFC 1918 private,
// loopback, multicast, broadcast, link-local, shared address space (RFC 6598),
// documentation/test ranges, benchmarking, protocol assignments, and reserved.
func validatePublicIP(ip [4]uint8) error {
	reject := func(reason string) error {
		return fmt.Errorf("IP address %d.%d.%d.%d is not publicly routable: %s", ip[0], ip[1], ip[2], ip[3], reason)
	}

	if ip == [4]uint8{0, 0, 0, 0} {
		return reject("unspecified")
	}

	// 0.0.0.0/8 "This network" (RFC 791)
	if ip[0] == 0 {
		return reject("0.0.0.0/8 (this network, RFC 791)")
	}

	// 127.0.0.0/8 loopback
	if ip[0] == 127 {
		return reject("127.0.0.0/8 (loopback)")
	}

	// Private: 10.0.0.0/8
	if ip[0] == 10 {
		return reject("10.0.0.0/8 (RFC 1918 private)")
	}

	// Private: 172.16.0.0/12
	if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
		return reject("172.16.0.0/12 (RFC 1918 private)")
	}

	// Private: 192.168.0.0/16
	if ip[0] == 192 && ip[1] == 168 {
		return reject("192.168.0.0/16 (RFC 1918 private)")
	}

	// Shared Address Space: 100.64.0.0/10 (RFC 6598)
	if ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127 {
		return reject("100.64.0.0/10 (shared address space, RFC 6598)")
	}

	// Link-local: 169.254.0.0/16
	if ip[0] == 169 && ip[1] == 254 {
		return reject("169.254.0.0/16 (link-local)")
	}

	// Protocol Assignments: 192.0.0.0/24 (RFC 6890)
	if ip[0] == 192 && ip[1] == 0 && ip[2] == 0 {
		return reject("192.0.0.0/24 (protocol assignments, RFC 6890)")
	}

	// Documentation: 192.0.2.0/24 TEST-NET-1 (RFC 5737)
	if ip[0] == 192 && ip[1] == 0 && ip[2] == 2 {
		return reject("192.0.2.0/24 (TEST-NET-1, RFC 5737)")
	}

	// Benchmarking: 198.18.0.0/15 (RFC 2544)
	if ip[0] == 198 && (ip[1] == 18 || ip[1] == 19) {
		return reject("198.18.0.0/15 (benchmarking, RFC 2544)")
	}

	// Documentation: 198.51.100.0/24 TEST-NET-2 (RFC 5737)
	if ip[0] == 198 && ip[1] == 51 && ip[2] == 100 {
		return reject("198.51.100.0/24 (TEST-NET-2, RFC 5737)")
	}

	// Documentation: 203.0.113.0/24 TEST-NET-3 (RFC 5737)
	if ip[0] == 203 && ip[1] == 0 && ip[2] == 113 {
		return reject("203.0.113.0/24 (TEST-NET-3, RFC 5737)")
	}

	// Multicast: 224.0.0.0/4
	if ip[0] >= 224 && ip[0] <= 239 {
		return reject("224.0.0.0/4 (multicast)")
	}

	// Broadcast: 255.255.255.255
	if ip == [4]uint8{255, 255, 255, 255} {
		return reject("255.255.255.255 (broadcast)")
	}

	// Reserved: 240.0.0.0/4 (future use)
	if ip[0] >= 240 {
		return reject("240.0.0.0/4 (reserved for future use)")
	}

	return nil
}
