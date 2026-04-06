//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// TestE2E_SentinelMulticastPublisherCreatesPublishers verifies that the sentinel's
// multicast publisher worker detects IBRL validators and creates multicast
// publisher users on-chain.
func TestE2E_SentinelMulticastPublisherCreatesPublishers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	deployDir := t.TempDir()
	deployID := "sentinel-mcast-" + random.ShortID()

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: deployDir,
		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
	}, logger, dockerClient, subnetAllocator)
	require.NoError(t, err)

	err = dn.Start(ctx, nil)
	t.Cleanup(func() { _ = dn.Destroy(context.Background(), false) })
	require.NoError(t, err)

	// Dump sentinel logs on test failure for debugging.
	t.Cleanup(func() {
		if t.Failed() {
			dn.DumpContainerLogs(t, "sentinel", dn.Sentinel.ContainerID)
			dn.DumpContainerLogs(t, "validator-metadata-service-mock", dn.ValidatorMetadataServiceMock.ContainerID)
			dn.DumpContainerLogs(t, "activator", dn.Activator.ContainerID)
		}
	})

	var multicastGroupPK string

	t.Run("create-device-and-multicast-group", func(t *testing.T) {
		// Create device (location, exchange, contributor already created by devnet init).
		devicePK, err := dn.GetOrCreateDeviceOnchain(ctx, "dev01", "ams", "xams", "", "45.33.100.1", []string{"45.33.100.8/29", "45.33.101.0/29"}, "mgmt")
		require.NoError(t, err)
		t.Logf("Device pubkey: %s", devicePK)

		// Register a user tunnel endpoint interface so the activator has a second
		// endpoint for multicast publishers (the IBRL users consume the device's
		// public IP endpoint).
		_, err = dn.Manager.Exec(ctx, []string{
			"doublezero", "device", "interface", "create", "dev01", "Loopback100",
			"--ip-net", "45.33.101.1/32",
			"--user-tunnel-endpoint", "true",
			"--bandwidth", "10G",
		})
		require.NoError(t, err)

		// Create a multicast group.
		output, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero multicast group create --code e2e-test --max-bandwidth 1Gbps --owner me -w"})
		require.NoError(t, err)

		// Parse multicast group pubkey.
		groupPK := parseMulticastGroupPK(t, output, dn, ctx)
		require.NotEmpty(t, groupPK, "multicast group pubkey should not be empty")
		multicastGroupPK = groupPK
		t.Logf("Multicast group pubkey: %s", multicastGroupPK)
	})

	t.Run("create-ibrl-users", func(t *testing.T) {
		ips := []string{"45.33.100.9", "45.33.100.10", "45.33.100.11"}
		for _, ip := range ips {
			_, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
				set -e
				doublezero access-pass set --client-ip ` + ip + ` --user-payer me
				doublezero user create --device dev01 --client-ip ` + ip + `
			`})
			require.NoError(t, err, "failed to create IBRL user for %s", ip)
			t.Logf("Created IBRL user for %s", ip)
		}
	})

	t.Run("start-validator-metadata-service-mock", func(t *testing.T) {
		err := dn.StartValidatorMetadataServiceMock(ctx)
		require.NoError(t, err)

		// Configure validator records for our test IPs.
		err = dn.ValidatorMetadataServiceMock.SetValidators(ctx, []devnet.ValidatorMetadataItem{
			{IP: "45.33.100.9", ActiveStake: 2000_000_000_000, VoteAccount: "vote1", SoftwareClient: "agave", SoftwareVersion: "1.0.0"},
			{IP: "45.33.100.10", ActiveStake: 1000_000_000_000, VoteAccount: "vote2", SoftwareClient: "agave", SoftwareVersion: "1.0.0"},
			{IP: "45.33.100.11", ActiveStake: 500_000_000_000, VoteAccount: "vote3", SoftwareClient: "agave", SoftwareVersion: "1.0.0"},
		})
		require.NoError(t, err)
	})

	t.Run("start-sentinel", func(t *testing.T) {
		dn.Spec.Sentinel.KeypairPath = dn.Spec.Manager.ManagerKeypairPath
		dn.Spec.Sentinel.MulticastGroupPubkeys = multicastGroupPK
		dn.Spec.Sentinel.MulticastPublisherPollSecs = 5

		err := dn.StartSentinel(ctx)
		require.NoError(t, err)
	})

	t.Run("wait-for-multicast-publishers", func(t *testing.T) {
		expectedIPs := []string{"45.33.100.9", "45.33.100.10", "45.33.100.11"}

		// Poll until the sentinel creates multicast publisher users for all 3 IPs.
		var multicastUsers []userListEntry
		err := poll.Until(ctx, func() (bool, error) {
			output, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
				"doublezero user list --user-type Multicast --all-tenants --json"})
			if err != nil {
				t.Logf("user list error (retrying): %v", err)
				return false, nil
			}

			if err := json.Unmarshal(output, &multicastUsers); err != nil {
				t.Logf("user list parse error (retrying): %v", err)
				return false, nil
			}

			activated := 0
			for _, u := range multicastUsers {
				if u.Status == "Activated" {
					activated++
				}
			}
			t.Logf("Multicast users: %d activated, %d total (expecting %d)", activated, len(multicastUsers), len(expectedIPs))
			return activated >= len(expectedIPs), nil
		}, 2*time.Minute, 5*time.Second)

		require.NoError(t, err, "timed out waiting for multicast publishers to be created")

		// Verify each multicast publisher user has the expected properties.
		usersByIP := make(map[string]userListEntry)
		for _, u := range multicastUsers {
			usersByIP[u.ClientIP] = u
		}

		for _, ip := range expectedIPs {
			u, ok := usersByIP[ip]
			require.True(t, ok, "expected multicast user for IP %s", ip)

			require.Equal(t, "Multicast", u.UserType, "user %s should be Multicast type", ip)
			require.Equal(t, "Activated", u.Status, "user %s should be Activated", ip)
			require.Contains(t, u.publishersList(), multicastGroupPK,
				"user %s should publish to the multicast group", ip)
			require.Equal(t, "dev01", u.DeviceName, "user %s should be on device dev01", ip)

			t.Logf("Verified multicast publisher: ip=%s status=%s device=%s publishers=%v", ip, u.Status, u.DeviceName, u.Publishers)
		}
	})
}

// parseMulticastGroupPK attempts to extract the multicast group pubkey from
// the create command output, falling back to a get command if needed.
func parseMulticastGroupPK(t *testing.T, output []byte, dn *devnet.Devnet, ctx context.Context) string {
	t.Helper()

	// Try parsing lines for a base58 pubkey.
	for _, line := range splitLines(string(output)) {
		if len(line) == 44 || len(line) == 43 {
			return line
		}
	}

	// Try JSON.
	var info struct {
		Account string `json:"account"`
	}
	if err := json.Unmarshal(output, &info); err == nil && info.Account != "" {
		return info.Account
	}

	// Fallback: get by code.
	output, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
		"doublezero multicast group get --code e2e-test --json"})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(output, &info))
	return info.Account
}

// userListEntry matches the JSON output of `doublezero user list --json`.
type userListEntry struct {
	Account    string `json:"account"`
	UserType   string `json:"user_type"`
	DeviceName string `json:"device_name"`
	ClientIP   string `json:"client_ip"`
	Status     string `json:"status"`
	Publishers string `json:"publishers"`
	Owner      string `json:"owner"`
}

// publishersList returns the publishers as a slice of strings.
func (u userListEntry) publishersList() []string {
	if u.Publishers == "" {
		return nil
	}
	parts := strings.Split(u.Publishers, ", ")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
