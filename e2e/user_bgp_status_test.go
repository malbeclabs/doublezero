//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/arista"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	serviceability "github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

func TestE2E_UserBGPStatus(t *testing.T) {
	t.Parallel()

	log := newTestLoggerForTest(t)

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	// Generate a telemetry keypair for the device's metrics publisher.
	telemetryKeypair := solana.NewWallet().PrivateKey
	telemetryKeypairJSON, _ := json.Marshal(telemetryKeypair[:])
	telemetryKeypairPath := t.TempDir() + "/dz1-telemetry-keypair.json"
	require.NoError(t, os.WriteFile(telemetryKeypairPath, telemetryKeypairJSON, 0600))
	telemetryKeypairPK := telemetryKeypair.PublicKey()

	minBalanceSOL := 3.0
	topUpSOL := 5.0
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  "dz-e2e-" + t.Name(),
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		DeviceTunnelNet: "192.168.100.0/24",
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Funder: devnet.FunderSpec{
			Verbose:       true,
			MinBalanceSOL: minBalanceSOL,
			TopUpSOL:      topUpSOL,
			Interval:      3 * time.Second,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)

	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:               "dz1",
		Location:           "ewr",
		Exchange:           "xewr",
		MetricsPublisherPK: telemetryKeypairPK.String(),
		// .8/29 has network address .8, allocatable up to .14, and broadcast .15
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
		LoopbackInterfaces: map[string]string{
			"Loopback255": "vpnv4",
			"Loopback256": "ipv4",
		},
		Telemetry: devnet.DeviceTelemetrySpec{
			Enabled:                  true,
			KeypairPath:              telemetryKeypairPath,
			ManagementNS:             "ns-management",
			Verbose:                  true,
			BGPStatusEnable:          true,
			BGPStatusInterval:        5 * time.Second,
			BGPStatusRefreshInterval: 1 * time.Hour,
		},
	})
	require.NoError(t, err)

	// Wait for the telemetry publisher to be funded before any onchain submissions are attempted.
	requireEventuallyFunded(t, log, dn.Ledger.GetRPCClient(), telemetryKeypairPK, minBalanceSOL, "telemetry publisher")

	client, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)

	tdn := &TestDevnet{Devnet: dn, log: log}

	t.Run("wait_for_device_activation", func(t *testing.T) {
		svcClient, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			data, err := svcClient.GetProgramData(t.Context())
			if err != nil {
				return false
			}
			for _, d := range data.Devices {
				if d.Code == device.Spec.Code && d.Status == serviceability.DeviceStatusActivated {
					return true
				}
			}
			return false
		}, 60*time.Second, 2*time.Second, "device was not activated within timeout")
	})

	if !t.Run("connect", func(t *testing.T) {
		tdn.ConnectIBRLUserTunnel(t, client)
		err := client.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)
	}) {
		t.FailNow()
	}

	if !t.Run("wait_for_user_activation", func(t *testing.T) {
		err := tdn.WaitForUserActivation(t, 1)
		require.NoError(t, err)
	}) {
		t.FailNow()
	}

	if !t.Run("wait_for_bgp_session_established", func(t *testing.T) {
		require.Eventually(t, func() bool {
			summary, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPBGPSummary](t.Context(), device, arista.ShowIPBGPSummaryCmd("vrf1"))
			if err != nil {
				log.Debug("bgp status: failed to fetch BGP summary", "error", err)
				return false
			}
			vrf, ok := summary.VRFs["vrf1"]
			if !ok {
				log.Debug("bgp status: vrf1 not found in BGP summary")
				return false
			}
			for ip, peer := range vrf.Peers {
				if peer.PeerState == "Established" {
					log.Debug("bgp status: BGP session established", "peer", ip)
					return true
				}
			}
			log.Debug("bgp status: no established BGP sessions yet", "peers", vrf.Peers)
			return false
		}, 90*time.Second, 5*time.Second, "BGP session never reached Established state")
	}) {
		t.FailNow()
	}

	if !t.Run("wait_for_bgp_status_up_onchain", func(t *testing.T) {
		svcClient, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			data, err := svcClient.GetProgramData(t.Context())
			if err != nil {
				log.Debug("bgp status: failed to fetch program data", "error", err)
				return false
			}
			for _, user := range data.Users {
				if user.Status != serviceability.UserStatusActivated {
					continue
				}
				if user.BgpStatus == uint8(serviceability.BGPStatusUp) {
					log.Debug("bgp status: user BGP status is Up onchain", "user", solana.PublicKeyFromBytes(user.PubKey[:]).String())
					return true
				}
				log.Debug("bgp status: user BGP status not yet Up", "bgpStatus", user.BgpStatus)
			}
			return false
		}, 60*time.Second, 5*time.Second, "user BGP status never reached Up onchain")
	}) {
		t.FailNow()
	}

	if !t.Run("disconnect", func(t *testing.T) {
		// Kill the doublezerod daemon ungracefully (SIGKILL) to simulate an
		// unexpected BGP session drop without triggering the onchain disconnect
		// lifecycle.  A clean disconnect via "doublezero disconnect" would delete
		// the user account onchain, leaving no record to check BGP status on.
		// With an ungraceful kill the user stays activated onchain, giving the
		// BGP status submitter a chance to detect the dropped session and submit Down.
		//
		// Ignore the error: killing doublezerod (PID 1) can tear down the
		// container, which terminates the exec session with exit 137 before
		// the "|| true" runs.
		client.Exec(t.Context(), []string{"bash", "-c", "pkill -9 doublezerod || true"}) //nolint:errcheck
	}) {
		t.FailNow()
	}

	if !t.Run("wait_for_bgp_status_down_onchain", func(t *testing.T) {
		svcClient, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			data, err := svcClient.GetProgramData(t.Context())
			if err != nil {
				log.Debug("bgp status: failed to fetch program data", "error", err)
				return false
			}
			for _, user := range data.Users {
				if user.Status != serviceability.UserStatusActivated {
					continue
				}
				if user.BgpStatus == uint8(serviceability.BGPStatusDown) {
					log.Debug("bgp status: user BGP status is Down onchain", "user", solana.PublicKeyFromBytes(user.PubKey[:]).String())
					return true
				}
				log.Debug("bgp status: user BGP status not yet Down", "bgpStatus", user.BgpStatus)
			}
			return false
		}, 60*time.Second, 5*time.Second, "user BGP status never reached Down onchain")
	}) {
		t.Fail()
	}
}

// TestE2E_UserBGPStatus_NonDefaultTenant verifies that the BGP status submitter
// correctly reports Up and Down for a user whose tunnel lives in a non-default
// VRF namespace (VrfId != 1), exercising the multi-namespace collection path
// added to support per-tenant VRF isolation.
func TestE2E_UserBGPStatus_NonDefaultTenant(t *testing.T) {
	t.Parallel()

	log := newTestLoggerForTest(t)

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	telemetryKeypair := solana.NewWallet().PrivateKey
	telemetryKeypairJSON, _ := json.Marshal(telemetryKeypair[:])
	telemetryKeypairPath := t.TempDir() + "/dz1-telemetry-keypair.json"
	require.NoError(t, os.WriteFile(telemetryKeypairPath, telemetryKeypairJSON, 0600))
	telemetryKeypairPK := telemetryKeypair.PublicKey()

	minBalanceSOL := 3.0
	topUpSOL := 5.0
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  "dz-e2e-" + t.Name(),
		DeployDir: t.TempDir(),
		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		DeviceTunnelNet: "192.168.100.0/24",
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Funder: devnet.FunderSpec{
			Verbose:       true,
			MinBalanceSOL: minBalanceSOL,
			TopUpSOL:      topUpSOL,
			Interval:      3 * time.Second,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)

	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:               "dz1",
		Location:           "ewr",
		Exchange:           "xewr",
		MetricsPublisherPK: telemetryKeypairPK.String(),
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
		LoopbackInterfaces: map[string]string{
			"Loopback255": "vpnv4",
			"Loopback256": "ipv4",
		},
		Telemetry: devnet.DeviceTelemetrySpec{
			Enabled:                  true,
			KeypairPath:              telemetryKeypairPath,
			ManagementNS:             "ns-management",
			Verbose:                  true,
			BGPStatusEnable:          true,
			BGPStatusInterval:        5 * time.Second,
			BGPStatusRefreshInterval: 1 * time.Hour,
		},
	})
	require.NoError(t, err)

	requireEventuallyFunded(t, log, dn.Ledger.GetRPCClient(), telemetryKeypairPK, minBalanceSOL, "telemetry publisher")

	client, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)

	tdn := &TestDevnet{Devnet: dn, log: log}

	t.Run("wait_for_device_activation", func(t *testing.T) {
		svcClient, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			data, err := svcClient.GetProgramData(t.Context())
			if err != nil {
				return false
			}
			for _, d := range data.Devices {
				if d.Code == device.Spec.Code && d.Status == serviceability.DeviceStatusActivated {
					return true
				}
			}
			return false
		}, 60*time.Second, 2*time.Second, "device was not activated within timeout")
	})

	// Create two tenants: the first consumes VrfId=1 (the default), so the
	// second ("tenant-alpha") receives VrfId=2, guaranteeing a non-default VRF.
	if !t.Run("create_tenants", func(t *testing.T) {
		_, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "tenant", "create", "--code", "tenant-placeholder"})
		require.NoError(t, err)
		_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "tenant", "create", "--code", "tenant-alpha"})
		require.NoError(t, err)
	}) {
		t.FailNow()
	}

	vrfID := getTenantVrfID(t, dn, "tenant-alpha")
	require.NotZero(t, vrfID, "tenant VRF ID must be non-zero")
	require.NotEqual(t, uint16(1), vrfID, "tenant VRF ID must differ from the default to exercise the multi-namespace path")
	vrfName := fmt.Sprintf("vrf%d", vrfID)

	if !t.Run("connect", func(t *testing.T) {
		_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --tenant tenant-alpha --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey,
		})
		require.NoError(t, err)

		_, err = client.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "tenant-alpha"})
		require.NoError(t, err)

		err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)
	}) {
		t.FailNow()
	}

	if !t.Run("wait_for_user_activation", func(t *testing.T) {
		err := tdn.WaitForUserActivation(t, 1)
		require.NoError(t, err)
	}) {
		t.FailNow()
	}

	if !t.Run("wait_for_bgp_session_established", func(t *testing.T) {
		require.Eventually(t, func() bool {
			summary, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPBGPSummary](t.Context(), device, arista.ShowIPBGPSummaryCmd(vrfName))
			if err != nil {
				log.Debug("bgp status: failed to fetch BGP summary", "vrf", vrfName, "error", err)
				return false
			}
			vrf, ok := summary.VRFs[vrfName]
			if !ok {
				log.Debug("bgp status: vrf not found in BGP summary", "vrf", vrfName)
				return false
			}
			for ip, peer := range vrf.Peers {
				if peer.PeerState == "Established" {
					log.Debug("bgp status: BGP session established", "vrf", vrfName, "peer", ip)
					return true
				}
			}
			log.Debug("bgp status: no established BGP sessions yet", "vrf", vrfName, "peers", vrf.Peers)
			return false
		}, 90*time.Second, 5*time.Second, "BGP session never reached Established state in "+vrfName)
	}) {
		t.FailNow()
	}

	if !t.Run("wait_for_bgp_status_up_onchain", func(t *testing.T) {
		svcClient, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			data, err := svcClient.GetProgramData(t.Context())
			if err != nil {
				log.Debug("bgp status: failed to fetch program data", "error", err)
				return false
			}
			for _, user := range data.Users {
				if user.Status != serviceability.UserStatusActivated {
					continue
				}
				if user.BgpStatus == uint8(serviceability.BGPStatusUp) {
					log.Debug("bgp status: user BGP status is Up onchain", "user", solana.PublicKeyFromBytes(user.PubKey[:]).String())
					return true
				}
				log.Debug("bgp status: user BGP status not yet Up", "bgpStatus", user.BgpStatus)
			}
			return false
		}, 60*time.Second, 5*time.Second, "user BGP status never reached Up onchain for non-default tenant")
	}) {
		t.FailNow()
	}

	if !t.Run("disconnect", func(t *testing.T) {
		client.Exec(t.Context(), []string{"bash", "-c", "pkill -9 doublezerod || true"}) //nolint:errcheck
	}) {
		t.FailNow()
	}

	if !t.Run("wait_for_bgp_status_down_onchain", func(t *testing.T) {
		svcClient, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			data, err := svcClient.GetProgramData(t.Context())
			if err != nil {
				log.Debug("bgp status: failed to fetch program data", "error", err)
				return false
			}
			for _, user := range data.Users {
				if user.Status != serviceability.UserStatusActivated {
					continue
				}
				if user.BgpStatus == uint8(serviceability.BGPStatusDown) {
					log.Debug("bgp status: user BGP status is Down onchain", "user", solana.PublicKeyFromBytes(user.PubKey[:]).String())
					return true
				}
				log.Debug("bgp status: user BGP status not yet Down", "bgpStatus", user.BgpStatus)
			}
			return false
		}, 60*time.Second, 5*time.Second, "user BGP status never reached Down onchain for non-default tenant")
	}) {
		t.Fail()
	}
}
