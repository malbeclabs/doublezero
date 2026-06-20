package devnet

import (
	"context"
	"fmt"
	"strings"
)

// poolOverlapMaxRetries bounds how many times createNetworkWithSubnet re-allocates a subnet
// after a Docker "pool overlaps" error before giving up. Each re-seed yields one fresh candidate;
// for docker-visible conflicts the allocator additionally skips up to docker.defaultRetries salted
// candidates within a single call, so the search widens further in that case. For the
// docker-invisible case the retry exists to handle, the effective search is ~poolOverlapMaxRetries
// distinct candidates.
const poolOverlapMaxRetries = 5

// subnetFinder allocates a collision-safe subnet for a given ID, skipping subnets already in use
// by existing docker networks. *docker.SubnetAllocator implements it.
type subnetFinder interface {
	FindAvailableSubnet(ctx context.Context, testID string) (string, error)
}

// isPoolOverlapErr reports whether err is the Docker daemon's address-pool collision error,
// raised when a network is created with a subnet that overlaps an existing one.
func isPoolOverlapErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Pool overlaps") || strings.Contains(msg, "invalid pool request")
}

// createNetworkWithSubnet allocates a collision-safe subnet from alloc (which skips subnets
// already in use by existing docker networks) and invokes create with it. On a Docker
// pool-overlap error — a TOCTOU race where a concurrent process grabbed the CIDR between
// allocation and creation — it re-allocates and retries, since the newly conflicting network
// now shows up in the allocator's docker scan and a different subnet is chosen. It returns the
// subnet that was successfully used.
func createNetworkWithSubnet(ctx context.Context, alloc subnetFinder, allocID string, create func(subnetCIDR string) error) (string, error) {
	var lastErr error
	for attempt := 0; attempt < poolOverlapMaxRetries; attempt++ {
		// FindAvailableSubnet is deterministic on its ID (it hashes ID+salt and only skips subnets
		// it can see in `docker network ls`). On a retry we hand it a different ID so it derives from
		// a different point in the hash space; otherwise, when the overlap source is invisible to the
		// docker scan (e.g. the daemon's default address pool, or a network created without an
		// explicit IPAM subnet), every attempt would re-derive the same colliding CIDR and the retry
		// would be a no-op. A re-seed is not guaranteed to avoid the failed CIDR for these invisible
		// overlaps — it just makes a different choice likely — but across the salted candidate space
		// it converges in practice. For overlaps the docker scan *can* see, the allocator skips the
		// now-visible conflicting subnet outright. Attempt 0 uses the bare ID so the common case
		// stays stable per deploy.
		allocSeed := allocID
		if attempt > 0 {
			allocSeed = fmt.Sprintf("%s#%d", allocID, attempt)
		}
		subnetCIDR, err := alloc.FindAvailableSubnet(ctx, allocSeed)
		if err != nil {
			return "", fmt.Errorf("failed to get available subnet: %w", err)
		}
		if err := create(subnetCIDR); err != nil {
			if isPoolOverlapErr(err) {
				lastErr = err
				continue
			}
			return "", err
		}
		return subnetCIDR, nil
	}
	return "", fmt.Errorf("failed to create network after %d attempts due to subnet pool overlap: %w", poolOverlapMaxRetries, lastErr)
}
