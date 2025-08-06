//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/prometheus"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	telemetrysdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestE2E_DeviceTelemetry(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	// Use the hardcoded serviceability program keypair for this test, since the telemetry program
	// is built with it as an expectation, and the initialize instruction will fail if the owner
	// of the devices/links is not the matching serviceability program ID.
	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	minBalanceSOL := 3.0
	topUpSOL := 5.0
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
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

	log.Info("==> Starting devnet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)

	// Add and start the 2 devices in parallel.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()

		// Generate a telemetry keypair.
		telemetryKeypair := solana.NewWallet().PrivateKey
		telemetryKeypairJSON, _ := json.Marshal(telemetryKeypair[:])
		telemetryKeypairPath := t.TempDir() + "/la2-dz01-telemetry-keypair.json"
		require.NoError(t, os.WriteFile(telemetryKeypairPath, telemetryKeypairJSON, 0600))
		telemetryKeypairPK := telemetryKeypair.PublicKey()

		// Add the la2-dz01 device.
		_, err = dn.AddDevice(t.Context(), devnet.DeviceSpec{
			Code:               "la2-dz01",
			Location:           "lax",
			Exchange:           "xlax",
			MetricsPublisherPK: telemetryKeypairPK.String(),
			// .8/29 has network address .8, allocatable up to .14, and broadcast .15
			CYOANetworkIPHostID:          8,
			CYOANetworkAllocatablePrefix: 29,
			Telemetry: devnet.DeviceTelemetrySpec{
				Enabled:     true,
				KeypairPath: telemetryKeypairPath,
				// NOTE: We intentionally do not set the management namespace here, so that we can
				// test the case where a device does not use one.
				ManagementNS:         "",
				TWAMPListenPort:      862,
				ProbeInterval:        1 * time.Second,
				SubmissionInterval:   5 * time.Second,
				PeersRefreshInterval: 5 * time.Second,
				Verbose:              true,
				MetricsEnable:        true,
				MetricsAddr:          "0.0.0.0:2114",
			},
		})
		require.NoError(t, err)

		// Wait for the telemetry publisher account to be funded.
		requireEventuallyFunded(t, log, dn.Ledger.GetRPCClient(), telemetryKeypairPK, minBalanceSOL, "telemetry publisher")
	}()
	go func() {
		defer wg.Done()

		// Generate a telemetry keypair.
		telemetryKeypair := solana.NewWallet().PrivateKey
		telemetryKeypairJSON, _ := json.Marshal(telemetryKeypair[:])
		telemetryKeypairPath := t.TempDir() + "/ny5-dz01-telemetry-keypair.json"
		require.NoError(t, os.WriteFile(telemetryKeypairPath, telemetryKeypairJSON, 0600))
		telemetryKeypairPK := telemetryKeypair.PublicKey()

		// Add the ny5-dz01 device.
		_, err = dn.AddDevice(t.Context(), devnet.DeviceSpec{
			Code:               "ny5-dz01",
			Location:           "ewr",
			Exchange:           "xewr",
			MetricsPublisherPK: telemetryKeypairPK.String(),
			// .16/29 has network address .16, allocatable up to .22, and broadcast .23
			CYOANetworkIPHostID:          16,
			CYOANetworkAllocatablePrefix: 29,
			Telemetry: devnet.DeviceTelemetrySpec{
				Enabled:              true,
				KeypairPath:          telemetryKeypairPath,
				ManagementNS:         "ns-management",
				TWAMPListenPort:      862,
				ProbeInterval:        1 * time.Second,
				SubmissionInterval:   5 * time.Second,
				PeersRefreshInterval: 5 * time.Second,
				Verbose:              true,
				MetricsEnable:        true,
				MetricsAddr:          "0.0.0.0:2114",
			},
		})
		require.NoError(t, err)

		// Wait for the telemetry publisher account to be funded.
		requireEventuallyFunded(t, log, dn.Ledger.GetRPCClient(), telemetryKeypairPK, minBalanceSOL, "telemetry publisher")
	}()
	wg.Wait()

	// Add some dummy devices onchain.
	log.Info("==> Adding dummy devices onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail

			doublezero device create --code ld4-dz01 --contributor co01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.72/29" --mgmt-vrf mgmt
			doublezero device create --code frk-dz01 --contributor co01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.88/29" --mgmt-vrf mgmt
			doublezero device create --code sg1-dz01 --contributor co01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "180.87.102.104/29" --mgmt-vrf mgmt
			doublezero device create --code ty2-dz01 --contributor co01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "180.87.154.112/29" --mgmt-vrf mgmt
			doublezero device create --code pit-dzd01 --contributor co01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32" --mgmt-vrf mgmt
			doublezero device create --code ams-dz001 --contributor co01 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "195.219.138.56/29" --mgmt-vrf mgmt

			# TODO: When the controller supports dzd metadata, this will have to be updated to reflect actual interfaces
			doublezero device interface create la2-dz01 "Switch1/1/1" physical
			doublezero device interface create ny5-dz01 "Switch1/1/1" physical
			doublezero device interface create ld4-dz01 "Switch1/1/1" physical
			doublezero device interface create frk-dz01 "Switch1/1/1" physical
			doublezero device interface create sg1-dz01 "Switch1/1/1" physical
			doublezero device interface create ty2-dz01 "Switch1/1/1" physical
			doublezero device interface create pit-dzd01 "Switch1/1/1" physical
			doublezero device interface create ams-dz001 "Switch1/1/1" physical
	`})
	require.NoError(t, err)

	log.Info("==> Creating links onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail

			doublezero link create --code "la2-dz01:ny5-dz01" --contributor co01 --side-a la2-dz01 --side-a-interface Switch1/1/1 --side-z ny5-dz01 --side-z-interface Switch1/1/1 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3
			doublezero link create --code "ny5-dz01:ld4-dz01" --contributor co01 --side-a ny5-dz01 --side-a-interface Switch1/1/1 --side-z ld4-dz01 --side-z-interface Switch1/1/1 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3
			doublezero link create --code "ld4-dz01:frk-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Switch1/1/1 --side-z frk-dz01 --side-z-interface Switch1/1/1 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10
			doublezero link create --code "ld4-dz01:sg1-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Switch1/1/1 --side-z sg1-dz01 --side-z-interface Switch1/1/1 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9
			doublezero link create --code "sg1-dz01:ty2-dz01" --contributor co01 --side-a sg1-dz01 --side-a-interface Switch1/1/1 --side-z ty2-dz01 --side-z-interface Switch1/1/1 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7
		`})
	require.NoError(t, err)

	// Manually create tunnel interfaces on the devices.
	// NOTE: This is a workaround until tunnels on devices are configured automatically when links
	// are created.
	la2ToNY5LinkTunnelLA2IP := "172.16.0.0" // 172.16.0.0/31 expected to be allocated to this link by the activator
	la2ToNY5LinkTunnelNY5IP := "172.16.0.1" // 172.16.0.0/31 expected to be allocated to this link by the activator
	ny5ToLD4LinkTunnelNY5IP := "172.16.0.2" // 172.16.0.2/31 expected to be allocated to this link by the activator
	func() {
		la2Device := dn.Devices["la2-dz01"]
		ny5Device := dn.Devices["ny5-dz01"]
		log.Info("==> Manually creating tunnel interfaces on the devices")
		la2Client, err := la2Device.GetEAPIHTTPClient()
		require.NoError(t, err)
		resp, err := la2Client.RunCommands([]string{
			"configure terminal",
			"interface Tunnel1",
			"ip address " + la2ToNY5LinkTunnelLA2IP + "/31",
			"tunnel mode gre",
			"tunnel source " + la2Device.CYOANetworkIP,
			"tunnel destination " + ny5Device.CYOANetworkIP,
			"no shutdown",
		}, "json")
		require.NoError(t, err)
		require.Nil(t, resp.Error)
		ny5Client, err := ny5Device.GetEAPIHTTPClient()
		require.NoError(t, err)
		resp, err = ny5Client.RunCommands([]string{
			"configure terminal",
			"interface Tunnel1",
			"ip address " + la2ToNY5LinkTunnelNY5IP + "/31",
			"tunnel mode gre",
			"tunnel source " + ny5Device.CYOANetworkIP,
			"tunnel destination " + la2Device.CYOANetworkIP,
			"no shutdown",
		}, "json")
		require.NoError(t, err)
		require.Nil(t, resp.Error)
		resp, err = ny5Client.RunCommands([]string{
			"configure terminal",
			"interface Tunnel2",
			"ip address " + ny5ToLD4LinkTunnelNY5IP + "/31",
			"tunnel mode gre",
			"tunnel source " + ny5Device.CYOANetworkIP,
			"tunnel destination 10.157.67.17", // non-existent device
			"no shutdown",
		}, "json")
		require.NoError(t, err)
		require.Nil(t, resp.Error)
	}()

	// Wait for the devices to be reachable from each other via their link tunnel using ICMP ping.
	log.Info("==> Waiting for devices to be reachable from each other via their link tunnel using ICMP ping")
	require.Eventually(t, func() bool {
		_, err := dn.Devices["la2-dz01"].Exec(t.Context(), []string{"ping", "-c", "1", "-w", "1", la2ToNY5LinkTunnelNY5IP})
		if err != nil {
			log.Debug("Waiting for la2-dz01 to be reachable from ny5-dz01 via tunnel", "error", err)
			return false
		}
		_, err = dn.Devices["ny5-dz01"].Exec(t.Context(), []string{"ping", "-c", "1", "-w", "1", la2ToNY5LinkTunnelLA2IP})
		if err != nil {
			log.Debug("Waiting for ny5-dz01 to be reachable from la2-dz01 via tunnel", "error", err)
			return false
		}
		return true
	}, 20*time.Second, 1*time.Second)

	// Before checking metrics, the la2 device is not using a management namespace, so we need to expose the metrics port via iptables.
	// This isn't needed for the ny5 device because it's using a management namespace and has a control plane ACL configured for it.
	la2InternalTelemetryMetricsPort, err := dn.Devices["la2-dz01"].InternalTelemetryMetricsPort()
	require.NoError(t, err)
	_, err = dn.Devices["la2-dz01"].Exec(t.Context(), []string{"iptables", "-I", "INPUT", "-p", "tcp", "--dport", strconv.Itoa(la2InternalTelemetryMetricsPort), "-j", "ACCEPT"})
	require.NoError(t, err)

	// Fetch metrics from both devices.
	la2MetricsClient := dn.Devices["la2-dz01"].GetTelemetryMetricsClient()
	require.NoError(t, la2MetricsClient.WaitForReady(t.Context(), 10*time.Second))
	err = la2MetricsClient.Fetch(t.Context())
	require.NoError(t, err)
	ny5MetricsClient := dn.Devices["ny5-dz01"].GetTelemetryMetricsClient()
	require.NoError(t, ny5MetricsClient.WaitForReady(t.Context(), 60*time.Second))
	err = ny5MetricsClient.Fetch(t.Context())
	require.NoError(t, err)

	// Get the post-reachability "tunnel not found" metric for the la2 device, so we can check that it doesn't increase from here at the end.
	la2TunnelNotFoundGaugeValues := la2MetricsClient.GetGaugeValues("doublezero_device_telemetry_agent_peer_discovery_not_found_tunnels")
	require.NotNil(t, la2TunnelNotFoundGaugeValues)
	prevLA2TunnelNotFoundCount := int(la2TunnelNotFoundGaugeValues[0].Value)

	// Get the post-reachability "tunnel not found" metric for the ny5 device, so we can check that it increases from here at the end.
	ny5TunnelNotFoundGaugeValues := ny5MetricsClient.GetGaugeValues("doublezero_device_telemetry_agent_peer_discovery_not_found_tunnels")
	require.NotNil(t, ny5TunnelNotFoundGaugeValues)
	prevNY5TunnelNotFoundCount := int(ny5TunnelNotFoundGaugeValues[0].Value)

	// Get post-startup "errors_total" metric for the la2 device, so we can check that it's 0 at the end.
	la2ErrorsCounterValues := la2MetricsClient.GetCounterValues("doublezero_device_telemetry_agent_errors_total")
	var prevLA2ErrorsCount int
	if la2ErrorsCounterValues != nil {
		prevLA2ErrorsCount = int(la2ErrorsCounterValues[0].Value)
	}
	ny5ErrorsCounterValues := ny5MetricsClient.GetCounterValues("doublezero_device_telemetry_agent_errors_total")
	var prevNY5ErrorsCount int
	if ny5ErrorsCounterValues != nil {
		prevNY5ErrorsCount = int(ny5ErrorsCounterValues[0].Value)
	}

	// Check that TWAMP probes work between the devices.
	log.Info("==> Checking that TWAMP probes work between the devices")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sender := dn.Devices["ny5-dz01"]
	reflector := dn.Devices["la2-dz01"]
	port := 1862
	_, err = sender.Exec(t.Context(), []string{"iptables", "-I", "INPUT", "-p", "udp", "--dport", strconv.Itoa(port), "-j", "ACCEPT"})
	require.NoError(t, err)
	_, err = reflector.Exec(t.Context(), []string{"iptables", "-I", "INPUT", "-p", "udp", "--dport", strconv.Itoa(port), "-j", "ACCEPT"})
	require.NoError(t, err)
	go func() {
		_, err = reflector.Exec(ctx, []string{"twamp-reflector", fmt.Sprintf(":%d", port)})
		require.NoError(t, err)
	}()
	require.Eventually(t, func() bool {
		_, err := reflector.Exec(t.Context(), []string{"bash", "-c", fmt.Sprintf("ss -uln '( dport = :%d )' | grep -q .", port)})
		return err == nil
	}, 3*time.Second, 100*time.Millisecond)
	output, err := sender.Exec(t.Context(), []string{"twamp-sender", "-q", "-local-addr", fmt.Sprintf("%s:%d", la2ToNY5LinkTunnelNY5IP, 0), "-remote-addr", fmt.Sprintf("%s:%d", la2ToNY5LinkTunnelLA2IP, port)})
	require.NoError(t, err)
	log.Info("TWAMP sender output", "output", string(output))
	require.Contains(t, string(output), "RTT:")
	rtt, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(string(output), "RTT: ")))
	require.NoError(t, err)
	require.Greater(t, rtt, 0*time.Millisecond)

	// Get devices and links from the serviceability program.
	log.Info("==> Waiting for devices and links to be available onchain")
	devices, links, _ := waitForDevicesAndLinks(t, dn, 8, 5, 30*time.Second)

	// Get the device and link public keys.
	la2Device, ok := devices["la2-dz01"]
	require.True(t, ok, "la2-dz01 device not found")
	la2DevicePK := solana.PublicKeyFromBytes(la2Device.PubKey[:])

	ny5Device, ok := devices["ny5-dz01"]
	require.True(t, ok, "ny5-dz01 device not found")
	ny5DevicePK := solana.PublicKeyFromBytes(ny5Device.PubKey[:])

	la2ToNy5Link, ok := links["la2-dz01:ny5-dz01"]
	require.True(t, ok, "la2-dz01:ny5-dz01 link not found")
	la2ToNy5LinkPK := solana.PublicKeyFromBytes(la2ToNy5Link.PubKey[:])

	// Check that the telemetry program is deployed.
	log.Info("==> Checking that telemetry program is deployed")
	isDeployed, err := dn.IsTelemetryProgramDeployed(t.Context())
	require.NoError(t, err)
	require.True(t, isDeployed)

	// Get the current ledger epoch.
	rpcClient := dn.Ledger.GetRPCClient()
	epochInfo, err := rpcClient.GetEpochInfo(t.Context(), solanarpc.CommitmentFinalized)
	require.NoError(t, err)
	epoch := epochInfo.Epoch

	// Check that the telemetry samples are being submitted to the telemetry program.
	log.Info("==> Checking that telemetry samples are being submitted to the telemetry program", "epoch", epoch)
	account, duration := waitForDeviceLatencySamples(t, dn, la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch, 1, 90*time.Second)
	log.Info("==> Got telemetry samples", "duration", duration, "epoch", account.Epoch, "originDevicePK", account.OriginDevicePK, "targetDevicePK", account.TargetDevicePK, "linkPK", account.LinkPK, "samplingIntervalMicroseconds", account.SamplingIntervalMicroseconds, "nextSampleIndex", account.NextSampleIndex, "samples", account.Samples)
	require.Greater(t, len(account.Samples), 1)
	require.Equal(t, len(account.Samples), int(account.NextSampleIndex))
	for _, rtt := range account.Samples {
		require.Greater(t, rtt, uint32(0))
	}
	prevAccount := account

	// Check that more samples are being submitted.
	// NOTE: We're assuming the epoch hasn't changed since the last batch of samples was
	// submitted, or else this test will fail.
	log.Info("==> Checking that more telemetry samples are being submitted to the telemetry program", "epoch", epoch)
	account, duration = waitForDeviceLatencySamples(t, dn, la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch, len(prevAccount.Samples), 90*time.Second)
	log.Info("==> Got telemetry samples", "duration", duration, "epoch", account.Epoch, "originDevicePK", account.OriginDevicePK, "targetDevicePK", account.TargetDevicePK, "linkPK", account.LinkPK, "samplingIntervalMicroseconds", account.SamplingIntervalMicroseconds, "nextSampleIndex", account.NextSampleIndex, "samples", account.Samples)
	require.Greater(t, len(account.Samples), len(prevAccount.Samples))
	require.Equal(t, prevAccount.StartTimestampMicroseconds, account.StartTimestampMicroseconds)
	require.Equal(t, prevAccount.SamplingIntervalMicroseconds, account.SamplingIntervalMicroseconds)
	require.Equal(t, len(account.Samples), int(account.NextSampleIndex))
	for _, rtt := range account.Samples {
		require.Greater(t, rtt, uint32(0))
	}
	require.Equal(t, prevAccount.Samples, account.Samples[:len(prevAccount.Samples)]) // same account and epoch, same samples prefix
	prevAccount = account

	// Get samples for the 2 active devices in other direction and check that they're all non-zero RTTs too.
	log.Info("==> Checking that telemetry samples are being submitted to the telemetry program in other direction", "epoch", epoch)
	account, duration = waitForDeviceLatencySamples(t, dn, ny5DevicePK, la2DevicePK, la2ToNy5LinkPK, epoch, 1, 90*time.Second)
	log.Info("==> Got telemetry samples", "duration", duration, "epoch", account.Epoch, "originDevicePK", account.OriginDevicePK, "targetDevicePK", account.TargetDevicePK, "linkPK", account.LinkPK, "samplingIntervalMicroseconds", account.SamplingIntervalMicroseconds, "nextSampleIndex", account.NextSampleIndex, "samples", account.Samples)
	require.Greater(t, len(account.Samples), 1)
	require.Equal(t, len(account.Samples), int(account.NextSampleIndex))
	for _, rtt := range account.Samples {
		require.Greater(t, rtt, uint32(0))
	}
	require.NotEqual(t, prevAccount.Samples, account.Samples) // different accounts, different samples

	// Get the dummy device and broken link public keys.
	ld4Device, ok := devices["ld4-dz01"]
	require.True(t, ok, "ld4-dz01 device not found")
	ld4DevicePK := solana.PublicKeyFromBytes(ld4Device.PubKey[:])

	ny5ToLd4Link, ok := links["ny5-dz01:ld4-dz01"]
	require.True(t, ok, "ny5-dz01:ld4-dz01 link not found")
	ny5ToLd4LinkPK := solana.PublicKeyFromBytes(ny5ToLd4Link.PubKey[:])

	// Get samples for link with dummy device and check that they're all 0 RTTs (losses).
	log.Info("==> Checking that telemetry samples are being submitted to the telemetry program for link with dummy device", "epoch", epoch)
	account, duration = waitForDeviceLatencySamples(t, dn, ny5DevicePK, ld4DevicePK, ny5ToLd4LinkPK, epoch, 1, 90*time.Second)
	log.Info("==> Got telemetry samples", "duration", duration, "epoch", account.Epoch, "originDevicePK", account.OriginDevicePK, "targetDevicePK", account.TargetDevicePK, "linkPK", account.LinkPK, "samplingIntervalMicroseconds", account.SamplingIntervalMicroseconds, "nextSampleIndex", account.NextSampleIndex, "samples", account.Samples)
	require.Greater(t, len(account.Samples), 1)
	require.Equal(t, len(account.Samples), int(account.NextSampleIndex))
	for _, rtt := range account.Samples {
		require.Equal(t, uint32(0), rtt)
	}

	// Fetch metrics from both devices.
	err = la2MetricsClient.Fetch(t.Context())
	require.NoError(t, err)
	err = ny5MetricsClient.Fetch(t.Context())
	require.NoError(t, err)

	// Check that la2 has 0 "tunnel not found" metric counted, since it has no links with non-existent devices.
	log.Info("==> Checking that la2 has 0 not found tunnels")
	la2NotFoundTunnelsGaugeValues := la2MetricsClient.GetGaugeValues("doublezero_device_telemetry_agent_peer_discovery_not_found_tunnels")
	require.NotNil(t, la2NotFoundTunnelsGaugeValues)
	require.Equal(t, 1, len(la2NotFoundTunnelsGaugeValues))
	require.Contains(t, la2NotFoundTunnelsGaugeValues[0].Labels, "local_device_pk")
	require.Equal(t, la2DevicePK.String(), la2NotFoundTunnelsGaugeValues[0].Labels["local_device_pk"])
	la2TNotFoundTunnelsCount := int(la2NotFoundTunnelsGaugeValues[0].Value)
	require.Equal(t, prevLA2TunnelNotFoundCount, la2TNotFoundTunnelsCount)

	// Check that ny5 has more than 0 "tunnel not found" metric counted, since it has a link with a non-existent device.
	log.Info("==> Checking that ny5 has more than 0 not found tunnels")
	ny5NotFoundTunnelsGaugeValues := ny5MetricsClient.GetGaugeValues("doublezero_device_telemetry_agent_peer_discovery_not_found_tunnels")
	require.NotNil(t, ny5NotFoundTunnelsGaugeValues)
	require.Equal(t, 1, len(ny5NotFoundTunnelsGaugeValues))
	require.Contains(t, ny5NotFoundTunnelsGaugeValues[0].Labels, "local_device_pk")
	require.Equal(t, ny5DevicePK.String(), ny5NotFoundTunnelsGaugeValues[0].Labels["local_device_pk"])
	ny5TNotFoundTunnelsCount := int(ny5NotFoundTunnelsGaugeValues[0].Value)
	require.Greater(t, ny5TNotFoundTunnelsCount, prevNY5TunnelNotFoundCount)

	// Check that the "errors_total" counter has not increased from startup.
	log.Info("==> Checking that errors_total counter has not increased from startup")
	la2ErrorsCounterValues = la2MetricsClient.GetCounterValues("doublezero_device_telemetry_agent_errors_total")
	if la2ErrorsCounterValues != nil {
		require.Equal(t, prevLA2ErrorsCount, int(la2ErrorsCounterValues[0].Value), "la2 errors_total should be 0: %v", la2ErrorsCounterValues)
	}
	ny5ErrorsCounterValues = ny5MetricsClient.GetCounterValues("doublezero_device_telemetry_agent_errors_total")
	if ny5ErrorsCounterValues != nil {
		require.Equal(t, prevNY5ErrorsCount, int(ny5ErrorsCounterValues[0].Value), "ny5 errors_total should be 0: %v", ny5ErrorsCounterValues)
	}

	// Check that go_memstats_alloc_bytes gauge is less than 3MB.
	log.Info("==> Checking that go_memstats_alloc_bytes gauge is less than 3MB on both devices")
	la2MemStatsAllocBytes := la2MetricsClient.GetGaugeValues(prometheus.MetricNameGoMemstatsAllocBytes)
	require.NotNil(t, la2MemStatsAllocBytes)
	require.Less(t, int(la2MemStatsAllocBytes[0].Value), int(3*1024*1024))
	ny5MemStatsAllocBytes := ny5MetricsClient.GetGaugeValues(prometheus.MetricNameGoMemstatsAllocBytes)
	require.NotNil(t, ny5MemStatsAllocBytes)
	require.Less(t, int(ny5MemStatsAllocBytes[0].Value), int(3*1024*1024))

	// Check that go_goroutines gauge is less than 20.
	log.Info("==> Checking that go_goroutines gauge is less than 30 on both devices")
	la2GoGoroutinesCounterValues := la2MetricsClient.GetGaugeValues(prometheus.MetricNameGoGoroutines)
	require.NotNil(t, la2GoGoroutinesCounterValues)
	require.Less(t, int(la2GoGoroutinesCounterValues[0].Value), 30)
	ny5GoGoroutinesCounterValues := ny5MetricsClient.GetGaugeValues(prometheus.MetricNameGoGoroutines)
	require.NotNil(t, ny5GoGoroutinesCounterValues)
	require.Less(t, int(ny5GoGoroutinesCounterValues[0].Value), 30)
}

func waitForDeviceLatencySamples(t *testing.T, dn *devnet.Devnet, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, waitForMinSamples int, timeout time.Duration) (*telemetrysdk.DeviceLatencySamples, time.Duration) {
	client, err := dn.Ledger.GetTelemetryClient(nil)
	require.NoError(t, err)

	start := time.Now()
	require.Eventually(t, func() bool {
		account, err := client.GetDeviceLatencySamples(t.Context(), originDevicePK, targetDevicePK, linkPK, epoch)
		if err != nil && !errors.Is(err, telemetrysdk.ErrAccountNotFound) {
			t.Fatalf("failed to get device latency samples: %v", err)
		}
		return account != nil && len(account.Samples) > waitForMinSamples
	}, timeout, 3*time.Second)

	account, err := client.GetDeviceLatencySamples(t.Context(), originDevicePK, targetDevicePK, linkPK, epoch)
	require.NoError(t, err)
	require.NotNil(t, account)

	return account, time.Since(start)
}

func waitForDevicesAndLinks(t *testing.T, dn *devnet.Devnet, expectedDevices, expectedLinks int, timeout time.Duration) (map[string]*serviceability.Device, map[string]*serviceability.Link, time.Duration) {
	client, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)

	start := time.Now()
	require.Eventually(t, func() bool {
		data, err := client.GetProgramData(t.Context())
		require.NoError(t, err)
		return len(data.Devices) == expectedDevices && len(data.Links) == expectedLinks
	}, timeout, 1*time.Second)

	data, err := client.GetProgramData(t.Context())
	require.NoError(t, err)

	links := map[string]*serviceability.Link{}
	for _, link := range data.Links {
		links[link.Code] = &link
	}

	devices := map[string]*serviceability.Device{}
	for _, device := range data.Devices {
		devices[device.Code] = &device
	}

	return devices, links, time.Since(start)
}
