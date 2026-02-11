package geoprobe

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

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
	if net.ParseIP(p.Host) == nil {
		return fmt.Errorf("host must be a valid IP address")
	}
	if p.Port == 0 {
		return fmt.Errorf("port cannot be zero")
	}
	return nil
}

// ParseProbeAddresses parses a comma-separated list of host:port values.
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
