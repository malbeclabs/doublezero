package geoprobe

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
)

// ProbeAddress represents a child geoProbe's network address.
type ProbeAddress struct {
	Host      string
	Port      uint16 // UDP offset port (used by Publisher)
	TWAMPPort uint16 // TWAMP reflector port (used by Pinger)
}

func (p ProbeAddress) String() string {
	return fmt.Sprintf("%s:%d:%d", p.Host, p.Port, p.TWAMPPort)
}

// Validate checks if the ProbeAddress has valid Host and Port values.
func (p ProbeAddress) Validate() error {
	if p.Host == "" {
		return fmt.Errorf("host cannot be empty")
	}
	if net.ParseIP(p.Host) == nil {
		return fmt.Errorf("host must be a valid IP address")
	}
	if p.Port == 0 {
		return fmt.Errorf("port cannot be zero")
	}
	if p.TWAMPPort == 0 {
		return fmt.Errorf("twamp port cannot be zero")
	}
	return nil
}

// ParseProbeAddresses parses a comma-separated list of probe addresses.
// Each entry is either host (default ports) or host:offset_port:twamp_port
// (both explicit). Two-field format (host:port) is rejected as ambiguous.
func ParseProbeAddresses(s string) ([]ProbeAddress, error) {
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

		fields := strings.Split(part, ":")
		var addr ProbeAddress

		switch len(fields) {
		case 1:
			host := fields[0]
			if net.ParseIP(host) == nil {
				return nil, fmt.Errorf("invalid probe address %q: invalid IP address", part)
			}
			addr = ProbeAddress{
				Host:      host,
				Port:      telemetryconfig.DefaultGeoprobeUDPPort,
				TWAMPPort: telemetryconfig.DefaultGeoprobeTWAMPPort,
			}

		case 3:
			host := fields[0]
			if net.ParseIP(host) == nil {
				return nil, fmt.Errorf("invalid probe address %q: invalid IP address", part)
			}
			port, err := strconv.ParseUint(fields[1], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid port in %q: %w", part, err)
			}
			if port == 0 {
				return nil, fmt.Errorf("invalid port 0 in %q", part)
			}
			twampPort, err := strconv.ParseUint(fields[2], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid twamp port in %q: %w", part, err)
			}
			if twampPort == 0 {
				return nil, fmt.Errorf("invalid twamp port 0 in %q", part)
			}
			addr = ProbeAddress{Host: host, Port: uint16(port), TWAMPPort: uint16(twampPort)}

		default:
			return nil, fmt.Errorf("invalid probe address %q: expected host or host:offset_port:twamp_port", part)
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
