//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// TestE2E_MultiTenantAccessControl verifies that the control plane rejects
// connections when tenant access is not authorized. This complements the
// data-plane isolation tests in TestE2E_MultiTenantVRF by testing that
// unauthorized connections are denied before any tunnel is established.
//
// Setup: 1 device, 2 clients, 2 tenants.
//   - client1: access pass with tenant-alpha in allowlist
//   - client2: access pass with no tenant (empty allowlist)
//
// Subtests:
//  1. cross_tenant_rejected — client1 tries tenant-bravo (not in its allowlist)
//  2. nonexistent_tenant_rejected — client1 tries a tenant that doesn't exist
//  3. no_allowlist_tenant_rejected — client2 (empty allowlist) tries tenant-alpha
//  4. correct_tenant_succeeds — client1 connects to tenant-alpha (positive control)
func TestE2E_MultiTenantAccessControl(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	log.Debug("==> Starting devnet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)
	log.Debug("--> Devnet started")

	// Add a single device.
	deviceCode := "la2-dz01"
	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:                         deviceCode,
		Location:                     "lax",
		Exchange:                     "xlax",
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
		LoopbackInterfaces:           map[string]string{"Loopback255": "vpnv4", "Loopback256": "ipv4"},
	})
	require.NoError(t, err)
	devicePK := device.ID
	log.Debug("--> Device added", "deviceCode", deviceCode, "devicePK", devicePK)

	// Wait for device to exist onchain.
	log.Debug("==> Waiting for device to exist onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		return len(data.Devices) == 1
	}, 30*time.Second, 1*time.Second)
	log.Debug("--> Device exists onchain")

	// Create two tenants.
	log.Debug("==> Creating tenants")
	_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "tenant", "create", "--code", "tenant-alpha"})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "tenant", "create", "--code", "tenant-bravo"})
	require.NoError(t, err)
	log.Debug("--> Tenants created")

	// Add two clients.
	log.Debug("==> Adding clients")
	client1, err := dn.AddClient(t.Context(), devnet.ClientSpec{CYOANetworkIPHostID: 100})
	require.NoError(t, err)
	log.Debug("--> client1 added", "pubkey", client1.Pubkey, "ip", client1.CYOANetworkIP)

	client2, err := dn.AddClient(t.Context(), devnet.ClientSpec{CYOANetworkIPHostID: 110})
	require.NoError(t, err)
	log.Debug("--> client2 added", "pubkey", client2.Pubkey, "ip", client2.CYOANetworkIP)

	// Wait for client latency results.
	log.Debug("==> Waiting for client latency results")
	err = client1.WaitForLatencyResults(t.Context(), devicePK, 90*time.Second)
	require.NoError(t, err)
	err = client2.WaitForLatencyResults(t.Context(), devicePK, 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Client latency results received")

	// Set access passes:
	// - client1: with tenant-alpha in allowlist
	// - client2: no tenant (empty allowlist)
	log.Debug("==> Setting access passes")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --tenant tenant-alpha --client-ip " + client1.CYOANetworkIP + " --user-payer " + client1.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client2.CYOANetworkIP + " --user-payer " + client2.Pubkey})
	require.NoError(t, err)
	log.Debug("--> Access passes set")

	// Subtest 1: Cross-tenant connection rejected.
	// client1 has access to tenant-alpha but tries to connect to tenant-bravo.
	// The onchain program should reject with TenantNotInAccessPassAllowlist (Custom error 79).
	if !t.Run("cross_tenant_rejected", func(t *testing.T) {
		log.Debug("==> Testing cross-tenant connection rejection")

		output, err := client1.Exec(t.Context(), []string{
			"doublezero", "connect", "ibrl", "tenant-bravo",
			"--client-ip", client1.CYOANetworkIP,
			"--device", deviceCode,
		}, docker.NoPrintOnError())
		require.Error(t, err, "connecting to unauthorized tenant should fail")

		outputStr := string(output)
		require.True(t,
			strings.Contains(outputStr, "Error creating user") ||
				strings.Contains(outputStr, "TenantNotInAccessPassAllowlist") ||
				strings.Contains(outputStr, "Tenant not in access-pass tenant_allowlist") ||
				strings.Contains(outputStr, "Custom(79)"),
			"expected tenant allowlist error, got: %s", outputStr,
		)

		// Verify client remains disconnected.
		status, err := client1.GetTunnelStatus(t.Context())
		require.NoError(t, err)
		require.Len(t, status, 1)
		require.Nil(t, status[0].DoubleZeroIP, "client should not have a DZ IP after rejected connection")

		log.Debug("--> Cross-tenant connection correctly rejected")
	}) {
		t.FailNow()
	}

	// Subtest 2: Nonexistent tenant rejected.
	// client1 tries to connect to a tenant that doesn't exist onchain.
	// The client-side preflight check should reject with "Tenant not found".
	if !t.Run("nonexistent_tenant_rejected", func(t *testing.T) {
		log.Debug("==> Testing nonexistent tenant rejection")

		output, err := client1.Exec(t.Context(), []string{
			"doublezero", "connect", "ibrl", "nonexistent-tenant-xyz",
			"--client-ip", client1.CYOANetworkIP,
			"--device", deviceCode,
		}, docker.NoPrintOnError())
		require.Error(t, err, "connecting to nonexistent tenant should fail")
		require.Contains(t, string(output), "Tenant not found",
			"expected 'Tenant not found' error, got: %s", string(output))

		// Verify client remains disconnected.
		status, err := client1.GetTunnelStatus(t.Context())
		require.NoError(t, err)
		require.Len(t, status, 1)
		require.Nil(t, status[0].DoubleZeroIP, "client should not have a DZ IP after rejected connection")

		log.Debug("--> Nonexistent tenant connection correctly rejected")
	}) {
		t.FailNow()
	}

	// Subtest 3: Empty allowlist + tenant specified rejected.
	// client2 has no tenants in its access pass allowlist but tries to connect
	// with tenant-alpha. The onchain program should reject with
	// TenantNotInAccessPassAllowlist (Custom error 79).
	if !t.Run("no_allowlist_tenant_rejected", func(t *testing.T) {
		log.Debug("==> Testing no-allowlist tenant rejection")

		output, err := client2.Exec(t.Context(), []string{
			"doublezero", "connect", "ibrl", "tenant-alpha",
			"--client-ip", client2.CYOANetworkIP,
			"--device", deviceCode,
		}, docker.NoPrintOnError())
		require.Error(t, err, "connecting with tenant when allowlist is empty should fail")

		outputStr := string(output)
		require.True(t,
			strings.Contains(outputStr, "Error creating user") ||
				strings.Contains(outputStr, "TenantNotInAccessPassAllowlist") ||
				strings.Contains(outputStr, "Tenant not in access-pass tenant_allowlist") ||
				strings.Contains(outputStr, "Custom(79)"),
			"expected tenant allowlist error, got: %s", outputStr,
		)

		// Verify client remains disconnected.
		status, err := client2.GetTunnelStatus(t.Context())
		require.NoError(t, err)
		require.Len(t, status, 1)
		require.Nil(t, status[0].DoubleZeroIP, "client should not have a DZ IP after rejected connection")

		log.Debug("--> No-allowlist tenant connection correctly rejected")
	}) {
		t.FailNow()
	}

	// Subtest 4: Correct tenant succeeds (positive control).
	// client1 connects to tenant-alpha, which IS in its allowlist.
	// This proves the test infrastructure works and the rejections above are
	// genuine access-control enforcement, not infrastructure failures.
	t.Run("correct_tenant_succeeds", func(t *testing.T) {
		log.Debug("==> Testing correct tenant connection succeeds")

		_, err := client1.Exec(t.Context(), []string{
			"doublezero", "connect", "ibrl", "tenant-alpha",
			"--client-ip", client1.CYOANetworkIP,
			"--device", deviceCode,
		})
		require.NoError(t, err, "connecting to authorized tenant should succeed")

		// Wait for tunnel to come up.
		err = client1.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err, "tunnel should come up for authorized tenant")

		// Verify tunnel is connected with a DZ IP.
		status, err := client1.GetTunnelStatus(t.Context())
		require.NoError(t, err)
		require.Len(t, status, 1)
		require.NotNil(t, status[0].DoubleZeroIP, "should have a DZ IP assigned")

		log.Debug("--> Correct tenant connection succeeded, disconnecting")

		// Disconnect and verify cleanup.
		_, err = client1.Exec(t.Context(), []string{
			"doublezero", "disconnect",
			"--client-ip", client1.CYOANetworkIP,
		})
		require.NoError(t, err)

		err = client1.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
		require.NoError(t, err)

		status, err = client1.GetTunnelStatus(t.Context())
		require.NoError(t, err)
		require.Len(t, status, 1)
		require.Nil(t, status[0].DoubleZeroIP, "DZ IP should be released after disconnect")

		log.Debug("--> Client disconnected and DZ IP released")
	})
}
