package awsreach

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"slices"
	"sort"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
)

// PrefixesURL is served over plain HTTP; the https URL redirects back to http.
const PrefixesURL = "http://ec2-reachability.amazonaws.com/prefixes-ipv4.json"

const pingTimeout = 3 * time.Second

type RegionTargets map[string][]string

// ParsePrefixes keys targets by exact region name. Local Zones such as us-west-2-lax-1 carry their own key.
func ParsePrefixes(r io.Reader) (RegionTargets, error) {
	var raw []map[string]map[string]string
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode prefixes document: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("prefixes document lists no regions")
	}

	parsed := make(map[string]map[string]net.IP)
	for _, entry := range raw {
		for region, cidrToAddress := range entry {
			for _, candidate := range cidrToAddress {
				ip := net.ParseIP(candidate)
				if ip == nil || ip.To4() == nil {
					continue
				}
				if parsed[region] == nil {
					parsed[region] = make(map[string]net.IP)
				}
				parsed[region][ip.String()] = ip.To4()
			}
		}
	}

	if len(parsed) == 0 {
		return nil, errors.New("prefixes document lists no regions with IPv4 addresses")
	}

	targets := make(RegionTargets, len(parsed))
	for region, byAddress := range parsed {
		ips := make([]net.IP, 0, len(byAddress))
		for _, ip := range byAddress {
			ips = append(ips, ip)
		}
		sort.Slice(ips, func(i, j int) bool { return bytes.Compare(ips[i], ips[j]) < 0 })

		addresses := make([]string, len(ips))
		for i, ip := range ips {
			addresses[i] = ip.String()
		}
		targets[region] = addresses
	}

	return targets, nil
}

func FetchPrefixes(ctx context.Context, client collector.HTTPClient, url string) (RegionTargets, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch prefixes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prefixes request failed with status: %d", resp.StatusCode)
	}

	return ParsePrefixes(resp.Body)
}

type PingFunc func(ctx context.Context, address string) bool

func SystemPing(ctx context.Context, address string) bool {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	return exec.CommandContext(ctx, "ping", "-n", "-c", "1", "-W", "2", address).Run() == nil
}

func PreferFirst(candidates []string, preferred string) []string {
	index := slices.Index(candidates, preferred)
	if index <= 0 {
		return candidates
	}

	reordered := make([]string, 0, len(candidates))
	reordered = append(reordered, candidates[index])
	reordered = append(reordered, candidates[:index]...)
	reordered = append(reordered, candidates[index+1:]...)
	return reordered
}

func FirstAnswering(ctx context.Context, candidates []string, ping PingFunc, maxAttempts int) (string, error) {
	attempts := 0
	for _, candidate := range candidates {
		if attempts >= maxAttempts {
			break
		}
		attempts++
		if ping(ctx, candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no address answered after %d attempts", attempts)
}
