//go:build qa

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/require"
)

// TestQA_DeviceProvisioning exercises the full device provisioning lifecycle
// as defined in rfcs/rfc12-network-provisioning.md:
//  1. Verify device is healthy (validates previous day's provisioning)
//  2. Delete interfaces, links, and device from ledger
//  3. Recreate device, interfaces, and links (gets new pubkey)
//  4. Restart agents with new pubkey via Ansible
//  5. Next day's run verifies device became healthy again
//
// Device state machine: Pending → DeviceProvisioning → LinkProvisioning → Activated
// Device health: Unknown → Pending → ReadyForLinks → ReadyForUsers
func TestQA_DeviceProvisioning(t *testing.T) {
	if envArg != "devnet" {
		t.Skip("skipping, only run on devnet for now")
	}

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

	t.Log("==> Verifying device is healthy (validates previous provisioning)")
	device, err := prov.GetDeviceByCode(ctx, deviceCode)
	require.NoError(t, err, "failed to get device %s", deviceCode)
	require.Equal(t, "ready-for-users", normalizeEnum(device.Health),
		"device health should be ready-for-users, got %s", device.Health)
	require.Equal(t, "activated", normalizeEnum(device.Status),
		"device status should be activated, got %s", device.Status)

	oldPubkey := device.Pubkey
	t.Logf("Current device pubkey: %s", oldPubkey)

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

	t.Log("==> Deleting links connected to device")
	for _, link := range links {
		t.Logf("Deleting link %s (pubkey: %s)", link.Code, link.Pubkey)
		err := prov.DeleteLink(ctx, link.Pubkey)
		require.NoError(t, err, "failed to delete link %s", link.Code)
	}

	t.Logf("==> Deleting %d interfaces on device", len(deviceConfig.Interfaces))
	for _, iface := range deviceConfig.Interfaces {
		t.Logf("Deleting interface %s", iface.Name)
		err := prov.DeleteInterface(ctx, deviceCode, iface.Name)
		require.NoError(t, err, "failed to delete interface %s", iface.Name)
	}

	// Wait for activator to close link accounts
	t.Log("==> Waiting for device reference count to reach zero")
	err = prov.WaitForRefCountZero(ctx, deviceCode)
	require.NoError(t, err, "timed out waiting for reference count to reach zero")

	t.Logf("==> Deleting device %s (pubkey: %s)", deviceCode, oldPubkey)
	err = prov.DeleteDevice(ctx, oldPubkey)
	require.NoError(t, err, "failed to delete device")

	t.Log("==> Recreating device")
	newPubkey, err := prov.CreateDevice(ctx, deviceConfig)
	require.NoError(t, err, "failed to create device")
	require.NotEqual(t, oldPubkey, newPubkey, "new pubkey should be different from old pubkey")
	t.Logf("New device pubkey: %s", newPubkey)

	// Recreate interfaces from captured config
	t.Logf("==> Creating %d interfaces", len(deviceConfig.Interfaces))
	for _, iface := range deviceConfig.Interfaces {
		loopbackType := iface.LoopbackType
		if loopbackType == "none" {
			loopbackType = ""
		}
		t.Logf("Creating interface %s (loopback-type: %s)", iface.Name, iface.LoopbackType)
		err = prov.CreateInterface(ctx, deviceCode, iface.Name, loopbackType)
		require.NoError(t, err, "failed to create interface %s", iface.Name)
	}

	t.Log("==> Recreating links")
	for _, link := range links {
		t.Logf("Creating link %s: %s/%s <-> %s/%s",
			link.Code, link.SideACode, link.SideAIfaceName, link.SideZCode, link.SideZIfaceName)

		err := prov.CreateLink(ctx, link)
		require.NoError(t, err, "failed to create link %s", link.Code)
	}

	t.Log("==> Setting device max-users and desired-status")
	err = prov.UpdateDevice(ctx, deviceCode, deviceConfig.MaxUsers, "activated")
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

// normalizeEnum lowercases and replaces underscores with dashes to produce a
// canonical kebab-case form regardless of whether the API returns PascalCase,
// snake_case, or kebab-case.
func normalizeEnum(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r == '_' {
			b.WriteByte('-')
		} else if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
