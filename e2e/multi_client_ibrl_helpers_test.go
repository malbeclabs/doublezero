//go:build e2e

package e2e_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/stretchr/testify/assert"
)

// requireClientHasRoutes asserts that a client's doublezero0 kernel route table contains
// every peer DZ IP within the timeout, dumping diagnostics and failing the test on timeout.
//
// Active route-liveness clients withhold the kernel route until the client↔client liveness
// handshake reaches Up, so these checks are the ones that flaked in #3949. On failure we
// emit the client's tunnel/route state so the flake is self-explaining.
func requireClientHasRoutes(t *testing.T, log *slog.Logger, name string, client *devnet.Client, timeout, interval time.Duration, wantPeerDZIPs ...string) {
	t.Helper()
	ok := assert.Eventually(t, func() bool {
		output, err := client.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		out := string(output)
		for _, ip := range wantPeerDZIPs {
			if !strings.Contains(out, ip) {
				return false
			}
		}
		return true
	}, timeout, interval, "%s should have routes to %s", name, strings.Join(wantPeerDZIPs, ", "))
	if ok {
		return
	}
	dumpClientRouteDiag(t, log, name, client, strings.Join(wantPeerDZIPs, ", "))
	t.Fatalf("%s should have routes to %s", name, strings.Join(wantPeerDZIPs, ", "))
}

// dumpClientRouteDiag emits tunnel/route state for a client when a route-convergence
// assertion times out. These checks otherwise emit nothing on failure, which made the
// #3949 flake slow to diagnose.
func dumpClientRouteDiag(t *testing.T, log *slog.Logger, name string, client *devnet.Client, peerDZIPs string) {
	t.Helper()
	// Use a fresh bounded context so a wedged container can't make the diagnostic
	// dump hang (the dump runs before t.Fatalf, while t.Context() is still live).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, cmd := range [][]string{
		{"doublezero", "status"},
		{"ip", "route", "show"},
		{"ip", "r", "list", "dev", "doublezero0"},
		// The daemon's /routes response carries the per-peer liveness_state,
		// liveness_state_reason, liveness_expected_kernel_state and liveness_peer_mode
		// fields, which say whether liveness (rather than BGP) is withholding the route.
		// --max-time bounds each curl: these commands share one 30s budget, and a hung
		// request against a wedged daemon would starve the iptables dump below, which is
		// the line that says whether the unblock actually took effect.
		{"curl", "-s", "--max-time", "5", "--unix-socket", "/var/run/doublezerod/doublezerod.sock", "http://doublezero/routes"},
		// Liveness counters from the daemon's prometheus endpoint (the e2e client
		// entrypoint runs doublezerod with -metrics-addr 0.0.0.0:8080). The `|| true`
		// keeps a no-match grep from being reported as a command error.
		{"bash", "-c", "curl -s --max-time 5 http://localhost:8080/metrics | grep -E '^doublezero_liveness_' || true"},
		// Packet counters show whether an unblock of the liveness UDP port took effect.
		{"iptables", "-L", "INPUT", "-n", "-v"},
	} {
		out, err := client.Exec(ctx, cmd)
		log.Error("route-diag",
			"client", name,
			"pubkey", client.Pubkey,
			"peerDZIPs", peerDZIPs,
			"cmd", strings.Join(cmd, " "),
			"output", string(out),
			"error", err,
		)
	}
}
