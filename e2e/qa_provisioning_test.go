//go:build qa

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/require"
)

// TestQA_DeviceProvisioning exercises the full device provisioning lifecycle
// as defined in rfcs/rfc12-network-provisioning.md:
//  1. Verify device is healthy (validates previous day's provisioning)
//  2. Delete device and links from ledger
//  3. Recreate device and links (gets new pubkey)
//  4. Restart agents with new pubkey via Ansible
//  5. Next day's run verifies device became healthy again
//
// Device state machine: Pending → DeviceProvisioning → LinkProvisioning → Activated
// Device health: Unknown → Pending → ReadyForLinks → ReadyForUsers
func TestQA_DeviceProvisioning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping provisioning test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	log := newTestLogger(t)

	// Validate required flags
	if deviceArg == "" {
		t.Fatal("The -device flag is required (e.g., -device=chi-dn-dzd4)")
	}
	if bmHostArg == "" {
		t.Fatal("The -bm-host flag is required (e.g., -bm-host=chi-dn-bm4)")
	}

	deviceCode := deviceArg
	bmHost := bmHostArg

	t.Logf("Starting provisioning test for device %s (CLI via SSH to %s)", deviceCode, bmHost)

	// Initialize provisioning test helper
	prov, err := qa.NewProvisioningTest(ctx, log, networkConfig, envArg, bmHost)
	require.NoError(t, err, "failed to create provisioning test")

	// Verify device is currently healthy (validates previous provisioning)
	t.Log("==> Verifying device is healthy (validates previous provisioning)")
	device, err := prov.GetDeviceByCode(ctx, deviceCode)
	require.NoError(t, err, "failed to get device %s", deviceCode)
	require.Equal(t, "ready-for-users", normalizeHealthStatus(device.Health),
		"device health should be ready-for-users, got %s", device.Health)
	require.Equal(t, "activated", normalizeStatus(device.Status),
		"device status should be activated, got %s", device.Status)

	oldPubkey := device.Pubkey
	t.Logf("Current device pubkey: %s", oldPubkey)

	// Capture device and link configuration before deletion
	t.Log("==> Capturing device and link configuration")
	deviceConfig, err := prov.CaptureDeviceConfig(ctx, device)
	require.NoError(t, err, "failed to capture device config")

	links, err := prov.GetLinksForDevice(ctx, deviceCode)
	require.NoError(t, err, "failed to get links for device")
	t.Logf("Found %d links connected to device", len(links))
	for _, link := range links {
		t.Logf("  - Link %s: %s/%s <-> %s/%s",
			link.Code, link.SideACode, link.SideAIfaceName, link.SideZCode, link.SideZIfaceName)
	}

	// Delete links connected to device
	t.Log("==> Deleting links connected to device")
	for _, link := range links {
		t.Logf("Deleting link %s (pubkey: %s)", link.Code, link.Pubkey)
		err := prov.DeleteLink(ctx, link.Pubkey)
		require.NoError(t, err, "failed to delete link %s", link.Code)
	}

	// Delete device
	t.Logf("==> Deleting device %s (pubkey: %s)", deviceCode, oldPubkey)
	err = prov.DeleteDevice(ctx, oldPubkey)
	require.NoError(t, err, "failed to delete device")

	// Recreate device
	t.Log("==> Recreating device")
	newPubkey, err := prov.CreateDevice(ctx, deviceConfig)
	require.NoError(t, err, "failed to create device")
	require.NotEqual(t, oldPubkey, newPubkey, "new pubkey should be different from old pubkey")
	t.Logf("New device pubkey: %s", newPubkey)

	// Create interfaces (Loopback255 for vpnv4, Loopback256 for ipv4)
	t.Log("==> Creating interfaces")
	err = prov.CreateInterface(ctx, deviceCode, "Loopback255", "vpnv4")
	require.NoError(t, err, "failed to create Loopback255 interface")
	err = prov.CreateInterface(ctx, deviceCode, "Loopback256", "ipv4")
	require.NoError(t, err, "failed to create Loopback256 interface")

	// Recreate links
	t.Log("==> Recreating links")
	for _, link := range links {
		t.Logf("Creating link %s: %s/%s <-> %s/%s",
			link.Code, link.SideACode, link.SideAIfaceName, link.SideZCode, link.SideZIfaceName)

		// Need to recreate the interface on the device side before creating the link
		if link.SideACode == deviceCode {
			err := prov.CreateInterface(ctx, deviceCode, link.SideAIfaceName, "")
			require.NoError(t, err, "failed to create interface %s on device", link.SideAIfaceName)
		}
		if link.SideZCode == deviceCode {
			err := prov.CreateInterface(ctx, deviceCode, link.SideZIfaceName, "")
			require.NoError(t, err, "failed to create interface %s on device", link.SideZIfaceName)
		}

		err := prov.CreateLink(ctx, link)
		require.NoError(t, err, "failed to create link %s", link.Code)
	}

	// Update device max-users and desired-status
	t.Log("==> Setting device max-users and desired-status")
	err = prov.UpdateDevice(ctx, newPubkey, deviceConfig.MaxUsers, "activated")
	require.NoError(t, err, "failed to update device")

	// Restart agents with new pubkey via Ansible
	// This updates both doublezero-agent and doublezero-telemetry daemons
	t.Log("==> Restarting agents with new pubkey via Ansible")
	err = prov.RunAnsibleAgentRestart(ctx, deviceCode, newPubkey)
	require.NoError(t, err, "failed to restart agents via Ansible")

	// Verify device was created with new pubkey
	t.Log("==> Verifying device was recreated")
	device, err = prov.GetDeviceByCode(ctx, deviceCode)
	require.NoError(t, err, "failed to get recreated device")
	require.Equal(t, newPubkey, device.Pubkey, "device pubkey should match new pubkey")

	t.Log("==> Provisioning complete!")
	t.Logf("Old pubkey: %s", oldPubkey)
	t.Logf("New pubkey: %s", newPubkey)
	t.Log("Device will transition: Pending -> DeviceProvisioning -> LinkProvisioning -> Activated")
	t.Log("Next run will verify device reached ready-for-users state")
}

func normalizeStatus(status string) string {
	// Handle both enum names and display values
	switch status {
	case "Activated", "activated":
		return "activated"
	case "Pending", "pending":
		return "pending"
	case "DeviceProvisioning", "device-provisioning":
		return "device-provisioning"
	case "LinkProvisioning", "link-provisioning":
		return "link-provisioning"
	case "Drained", "drained":
		return "drained"
	default:
		return status
	}
}

func normalizeHealthStatus(health string) string {
	// Handle both enum names and display values
	switch health {
	case "ReadyForUsers", "ready-for-users":
		return "ready-for-users"
	case "ReadyForLinks", "ready-for-links":
		return "ready-for-links"
	case "Pending", "pending":
		return "pending"
	case "Unknown", "unknown":
		return "unknown"
	case "Impaired", "impaired":
		return "impaired"
	default:
		return health
	}
}
