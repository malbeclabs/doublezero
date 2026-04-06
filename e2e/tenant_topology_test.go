//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// TestE2E_FlexAlgo_TenantIncludeTopologies verifies that a foundation operator can
// assign and clear topology filters on a tenant account via
// `doublezero tenant update --include-topologies`.
func TestE2E_FlexAlgo_TenantIncludeTopologies(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	// This test only needs the ledger and manager — no activator, no cEOS.
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),
		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Activator: devnet.ActivatorSpec{Disabled: devnet.BoolPtr(true)},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)

	// Create a topology to use for include_topologies assignment.
	// AdminGroupBits is created during global-config set (devnet init), so
	// topology create can proceed without any extra setup.
	log.Debug("==> Creating tenant-topo topology")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "create",
		"--name", "tenant-topo",
		"--constraint", "include-any",
	})
	require.NoError(t, err)

	// Create a tenant.
	log.Debug("==> Creating tenant")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "tenant", "create",
		"--code", "test-tenant",
	})
	require.NoError(t, err)

	// Wait for the tenant to appear onchain.
	require.Eventually(t, func() bool {
		data, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		for _, tenant := range data.Tenants {
			if tenant.Code == "test-tenant" {
				return true
			}
		}
		return false
	}, 30*time.Second, 2*time.Second, "tenant did not appear onchain")

	// Confirm include_topologies starts empty.
	data, err := serviceabilityClient.GetProgramData(ctx)
	require.NoError(t, err)
	for _, tenant := range data.Tenants {
		if tenant.Code == "test-tenant" {
			require.Empty(t, tenant.IncludeTopologies, "include_topologies should start empty")
		}
	}

	// Assign the topology to the tenant.
	log.Debug("==> Setting include_topologies = tenant-topo")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "tenant", "update",
		"--pubkey", "test-tenant",
		"--include-topologies", "tenant-topo",
	})
	require.NoError(t, err)

	// Verify include_topologies is now set (one entry).
	require.Eventually(t, func() bool {
		d, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		for _, tenant := range d.Tenants {
			if tenant.Code == "test-tenant" {
				return len(tenant.IncludeTopologies) == 1
			}
		}
		return false
	}, 30*time.Second, 2*time.Second, "include_topologies was not set")
	log.Debug("--> include_topologies = [tenant-topo] confirmed")

	// Clear include_topologies by setting it to "default".
	log.Debug("==> Clearing include_topologies")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "tenant", "update",
		"--pubkey", "test-tenant",
		"--include-topologies", "default",
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		d, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		for _, tenant := range d.Tenants {
			if tenant.Code == "test-tenant" {
				return len(tenant.IncludeTopologies) == 0
			}
		}
		return false
	}, 30*time.Second, 2*time.Second, "include_topologies did not clear")
	log.Debug("--> include_topologies cleared")
}
