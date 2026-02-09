//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	telemetrysdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_DeviceTelemetry(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

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
		DeviceTunnelNet: "192.168.99.0/24",
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Funder: devnet.FunderSpec{
			Verbose:       true,
			MinBalanceSOL: minBalanceSOL,
			TopUpSOL:      topUpSOL,
			Interval:      3 * time.Second,
		},
		InfluxDB:           devnet.InfluxDBSpec{Enabled: true},
		Prometheus:         devnet.PrometheusSpec{Enabled: true},
		DeviceHealthOracle: devnet.DeviceHealthOracleSpec{Enabled: true},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	log.Debug("==> Starting devnet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)

	linkNetwork := devnet.NewMiscNetwork(dn, log, "la2-dz01:ny5-dz01")
	_, err = linkNetwork.CreateIfNotExists(t.Context())
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
			AdditionalNetworks: []string{linkNetwork.Name},
			Interfaces: map[string]string{
				"Ethernet2": "physical",
			},
			LoopbackInterfaces: map[string]string{
				"Loopback255": "vpnv4",
				"Loopback256": "ipv4",
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
			AdditionalNetworks: []string{linkNetwork.Name},
			Interfaces: map[string]string{
				"Ethernet2": "physical",
				"Ethernet3": "physical",
			},
			LoopbackInterfaces: map[string]string{
				"Loopback255": "vpnv4",
				"Loopback256": "ipv4",
			},
		})
		require.NoError(t, err)

		// Wait for the telemetry publisher account to be funded.
		requireEventuallyFunded(t, log, dn.Ledger.GetRPCClient(), telemetryKeypairPK, minBalanceSOL, "telemetry publisher")
	}()
	wg.Wait()

	// Add some dummy devices onchain.
	log.Debug("==> Adding dummy devices onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			doublezero device create --code ld4-dz01 --contributor co01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.80/29" --mgmt-vrf mgmt --desired-status activated
			doublezero device create --code frk-dz01 --contributor co01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.96/29" --mgmt-vrf mgmt --desired-status activated
			doublezero device create --code sg1-dz01 --contributor co01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "180.87.102.112/29" --mgmt-vrf mgmt --desired-status activated
			doublezero device create --code ty2-dz01 --contributor co01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "180.87.154.120/29" --mgmt-vrf mgmt --desired-status activated
			doublezero device create --code pit-dzd01 --contributor co01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32" --mgmt-vrf mgmt --desired-status activated
			doublezero device create --code ams-dz001 --contributor co01 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "195.219.138.56/29" --mgmt-vrf mgmt --desired-status activated
			doublezero device update --pubkey ams-dz001 --desired-status activated

			doublezero device interface create ld4-dz01 "Ethernet2"
			doublezero device interface create ld4-dz01 "Ethernet3"
			doublezero device interface create ld4-dz01 "Ethernet4"
			doublezero device interface create frk-dz01 "Ethernet2"
			doublezero device interface create sg1-dz01 "Ethernet2"
			doublezero device interface create sg1-dz01 "Ethernet3"
			doublezero device interface create ty2-dz01 "Ethernet2"
			doublezero device interface create pit-dzd01 "Ethernet2"
			doublezero device interface create ams-dz001 "Ethernet2"

			doublezero device interface create ld4-dz01 "Loopback255" --loopback-type vpnv4
			doublezero device interface create frk-dz01 "Loopback255" --loopback-type vpnv4
			doublezero device interface create sg1-dz01 "Loopback255" --loopback-type vpnv4
			doublezero device interface create ty2-dz01 "Loopback255" --loopback-type vpnv4
			doublezero device interface create pit-dzd01 "Loopback255" --loopback-type vpnv4
			doublezero device interface create ams-dz001 "Loopback255" --loopback-type vpnv4

			doublezero device interface create ld4-dz01 "Loopback256" --loopback-type ipv4
			doublezero device interface create frk-dz01 "Loopback256" --loopback-type ipv4
			doublezero device interface create sg1-dz01 "Loopback256" --loopback-type ipv4
			doublezero device interface create ty2-dz01 "Loopback256" --loopback-type ipv4
			doublezero device interface create pit-dzd01 "Loopback256" --loopback-type ipv4
			doublezero device interface create ams-dz001 "Loopback256" --loopback-type ipv4

			doublezero device update --pubkey ld4-dz01 --max-users 128
			doublezero device update --pubkey frk-dz01 --max-users 128
			doublezero device update --pubkey sg1-dz01 --max-users 128
			doublezero device update --pubkey ty2-dz01 --max-users 128
			doublezero device update --pubkey pit-dzd01 --max-users 128
			doublezero device update --pubkey ams-dz001 --max-users 128

`})
	require.NoError(t, err)

	log.Debug("==> Creating links onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail

			doublezero link create wan --code "la2-dz01:ny5-dz01" --contributor co01 --side-a la2-dz01 --side-a-interface Ethernet2 --side-z ny5-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3 --desired-status activated
			doublezero link create wan --code "ny5-dz01:ld4-dz01" --contributor co01 --side-a ny5-dz01 --side-a-interface Ethernet3 --side-z ld4-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3 --desired-status activated
			doublezero link create wan --code "ld4-dz01:frk-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Ethernet3 --side-z frk-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10 --desired-status activated
			doublezero link create wan --code "ld4-dz01:sg1-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Ethernet4 --side-z sg1-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9 --desired-status activated
			doublezero link create wan --code "sg1-dz01:ty2-dz01" --contributor co01 --side-a sg1-dz01 --side-a-interface Ethernet3 --side-z ty2-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7 --desired-status activated
		`})
	require.NoError(t, err)

	var la2ToNY5LinkTunnelLA2IP, la2ToNY5LinkTunnelNY5IP string
	log.Debug("==> Waiting for interfaces to be created on the devices")
	require.Eventually(t, func() bool {
		la2Device := dn.Devices["la2-dz01"]
		ny5Device := dn.Devices["ny5-dz01"]

		client, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)

		data, err := client.GetProgramData(t.Context())
		require.NoError(t, err)

		devices := make(map[string]*serviceability.Device)
		for _, device := range data.Devices {
			devices[device.Code] = &device
		}
		if _, ok := devices[la2Device.Spec.Code]; !ok {
			log.Debug("Waiting for la2-dz01 device to be found in program data", "la2DeviceCode", la2Device.Spec.Code)
			return false
		}
		if _, ok := devices[ny5Device.Spec.Code]; !ok {
			log.Debug("Waiting for ny5-dz01 device to be found in program data", "ny5DeviceCode", ny5Device.Spec.Code)
			return false
		}

		la2Interfaces := make(map[string]serviceability.Interface)
		for _, iface := range devices[la2Device.Spec.Code].Interfaces {
			la2Interfaces[iface.Name] = iface
		}
		ny5Interfaces := make(map[string]serviceability.Interface)
		for _, iface := range devices[ny5Device.Spec.Code].Interfaces {
			ny5Interfaces[iface.Name] = iface
		}

		if _, ok := la2Interfaces["Ethernet2"]; !ok {
			log.Debug("Waiting for la2-dz01 to have an Ethernet2 interface", "la2Interfaces", la2Interfaces)
			return false
		}
		if _, ok := ny5Interfaces["Ethernet2"]; !ok {
			log.Debug("Waiting for ny5-dz01 to have an Ethernet2 interface", "ny5Interfaces", ny5Interfaces)
			return false
		}
		if la2Interfaces["Ethernet2"].IpNet == [5]uint8{} {
			log.Debug("Waiting for la2-dz01 to have an Ethernet2 interface with an IP", "la2Interfaces", la2Interfaces)
			return false
		}
		if ny5Interfaces["Ethernet2"].IpNet == [5]uint8{} {
			log.Debug("Waiting for ny5-dz01 to have an Ethernet2 interface with an IP", "ny5Interfaces", ny5Interfaces)
			return false
		}
		la2ToNY5LinkTunnelLA2IP = bytesToIP4Net(la2Interfaces["Ethernet2"].IpNet).IP.String()
		la2ToNY5LinkTunnelNY5IP = bytesToIP4Net(ny5Interfaces["Ethernet2"].IpNet).IP.String()

		return true
	}, 120*time.Second, 3*time.Second, "Timed out waiting for the devices to be reachable via their link tunnel")

	// Wait for the devices to be reachable from each other via their link tunnel using TWAMP UDP probes.
	log.Debug("==> Waiting for devices to be reachable from each other via their link tunnel using TWAMP")
	require.Eventually(t, func() bool {
		_, err := dn.Devices["la2-dz01"].Exec(t.Context(), []string{"twamp-sender", "-q", "-local-addr", fmt.Sprintf("%s:%d", la2ToNY5LinkTunnelLA2IP, 0), "-remote-addr", fmt.Sprintf("%s:%d", la2ToNY5LinkTunnelNY5IP, 862)})
		if err != nil {
			log.Debug("Waiting for la2-dz01 to be reachable from ny5-dz01 via tunnel", "error", err)
			return false
		}
		_, err = dn.Devices["ny5-dz01"].Exec(t.Context(), []string{"twamp-sender", "-q", "-local-addr", fmt.Sprintf("%s:%d", la2ToNY5LinkTunnelNY5IP, 0), "-remote-addr", fmt.Sprintf("%s:%d", la2ToNY5LinkTunnelLA2IP, 862)})
		if err != nil {
			log.Debug("Waiting for ny5-dz01 to be reachable from la2-dz01 via tunnel", "error", err)
			return false
		}
		return true
	}, 300*time.Second, 3*time.Second)

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
	log.Debug("==> Checking that TWAMP probes work between the devices")
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
	var output []byte
	require.Eventually(t, func() bool {
		var sendErr error
		output, sendErr = sender.Exec(t.Context(), []string{"twamp-sender", "-q", "-local-addr", fmt.Sprintf("%s:%d", la2ToNY5LinkTunnelNY5IP, 0), "-remote-addr", fmt.Sprintf("%s:%d", la2ToNY5LinkTunnelLA2IP, port)})
		if sendErr != nil {
			log.Debug("Waiting for TWAMP sender to succeed", "error", sendErr)
			return false
		}
		return true
	}, 30*time.Second, 1*time.Second, "TWAMP sender should succeed")
	log.Debug("TWAMP sender output", "output", string(output))
	require.Contains(t, string(output), "RTT:")
	rtt, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(string(output), "RTT: ")))
	require.NoError(t, err)
	require.Greater(t, rtt, 0*time.Millisecond)

	// Get devices and links from the serviceability program.
	log.Debug("==> Waiting for devices and links to be available onchain")
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
	log.Debug("==> Checking that telemetry program is deployed")
	isDeployed, err := dn.IsTelemetryProgramDeployed(t.Context())
	require.NoError(t, err)
	require.True(t, isDeployed)

	// Get the current ledger epoch.
	rpcClient := dn.Ledger.GetRPCClient()
	epochInfo, err := rpcClient.GetEpochInfo(t.Context(), solanarpc.CommitmentFinalized)
	require.NoError(t, err)
	epoch := epochInfo.Epoch

	// Check that the telemetry samples are being submitted to the telemetry program.
	log.Debug("==> Checking that telemetry samples are being submitted to the telemetry program", "epoch", epoch)
	account, duration := waitForDeviceLatencySamples(t, dn, la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch, 16, true, 120*time.Second)
	log.Debug("==> Got telemetry samples", "duration", duration, "epoch", account.Epoch, "originDevicePK", account.OriginDevicePK, "targetDevicePK", account.TargetDevicePK, "linkPK", account.LinkPK, "samplingIntervalMicroseconds", account.SamplingIntervalMicroseconds, "nextSampleIndex", account.NextSampleIndex, "samples", account.Samples)
	require.Greater(t, len(account.Samples), 1)
	require.Equal(t, len(account.Samples), int(account.NextSampleIndex))
	// If there are 0s, they should be at the beginning of the samples array, with all non-zero values after them.
	initialZeroCount := 0
	for _, rtt := range account.Samples {
		if rtt == 0 {
			initialZeroCount++
		}
	}
	require.Less(t, initialZeroCount, len(account.Samples))
	for _, rtt := range account.Samples[initialZeroCount:] {
		require.Greater(t, rtt, uint32(0))
	}
	prevAccount := account

	// Check that more samples are being submitted.
	// NOTE: We're assuming the epoch hasn't changed since the last batch of samples was
	// submitted, or else this test will fail.
	log.Debug("==> Checking that more telemetry samples are being submitted to the telemetry program", "epoch", epoch)
	account, duration = waitForDeviceLatencySamples(t, dn, la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch, len(prevAccount.Samples), true, 120*time.Second)
	log.Debug("==> Got telemetry samples", "duration", duration, "epoch", account.Epoch, "originDevicePK", account.OriginDevicePK, "targetDevicePK", account.TargetDevicePK, "linkPK", account.LinkPK, "samplingIntervalMicroseconds", account.SamplingIntervalMicroseconds, "nextSampleIndex", account.NextSampleIndex, "samples", account.Samples)
	require.Greater(t, len(account.Samples), len(prevAccount.Samples))
	require.Equal(t, prevAccount.StartTimestampMicroseconds, account.StartTimestampMicroseconds)
	require.Equal(t, prevAccount.SamplingIntervalMicroseconds, account.SamplingIntervalMicroseconds)
	require.Equal(t, len(account.Samples), int(account.NextSampleIndex))
	for _, rtt := range account.Samples[initialZeroCount:] {
		require.Greater(t, rtt, uint32(0))
	}
	require.Equal(t, prevAccount.Samples, account.Samples[:len(prevAccount.Samples)]) // same account and epoch, same samples prefix
	prevAccount = account

	// Get samples for the 2 active devices in other direction and check that they're all non-zero RTTs too.
	log.Debug("==> Checking that telemetry samples are being submitted to the telemetry program in other direction", "epoch", epoch)
	account, duration = waitForDeviceLatencySamples(t, dn, ny5DevicePK, la2DevicePK, la2ToNy5LinkPK, epoch, 16, true, 120*time.Second)
	log.Debug("==> Got telemetry samples", "duration", duration, "epoch", account.Epoch, "originDevicePK", account.OriginDevicePK, "targetDevicePK", account.TargetDevicePK, "linkPK", account.LinkPK, "samplingIntervalMicroseconds", account.SamplingIntervalMicroseconds, "nextSampleIndex", account.NextSampleIndex, "samples", account.Samples)
	require.Greater(t, len(account.Samples), 1)
	require.Equal(t, len(account.Samples), int(account.NextSampleIndex))
	// If there are 0s, they should be at the beginning of the samples array, with all non-zero values after them.
	initialZeroCount = 0
	for _, rtt := range account.Samples {
		if rtt == 0 {
			initialZeroCount++
		}
	}
	require.Less(t, initialZeroCount, len(account.Samples))
	for _, rtt := range account.Samples[initialZeroCount:] {
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
	log.Debug("==> Checking that telemetry samples are being submitted to the telemetry program for link with dummy device", "epoch", epoch)
	account, duration = waitForDeviceLatencySamples(t, dn, ny5DevicePK, ld4DevicePK, ny5ToLd4LinkPK, epoch, 10, false, 120*time.Second)
	log.Debug("==> Got telemetry samples", "duration", duration, "epoch", account.Epoch, "originDevicePK", account.OriginDevicePK, "targetDevicePK", account.TargetDevicePK, "linkPK", account.LinkPK, "samplingIntervalMicroseconds", account.SamplingIntervalMicroseconds, "nextSampleIndex", account.NextSampleIndex, "samples", account.Samples)
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

	// Check that la2 has 0 "tunnel not found" gauge metric value, since it has no links with non-existent devices.
	log.Debug("==> Checking that la2 has 0 not found tunnels")
	la2NotFoundTunnelsGaugeValues := la2MetricsClient.GetGaugeValues("doublezero_device_telemetry_agent_peer_discovery_not_found_tunnels")
	require.NotNil(t, la2NotFoundTunnelsGaugeValues)
	require.Equal(t, 1, len(la2NotFoundTunnelsGaugeValues))
	require.Contains(t, la2NotFoundTunnelsGaugeValues[0].Labels, "local_device_pk")
	require.Equal(t, la2DevicePK.String(), la2NotFoundTunnelsGaugeValues[0].Labels["local_device_pk"])
	la2TNotFoundTunnelsCount := int(la2NotFoundTunnelsGaugeValues[0].Value)
	require.Equal(t, 0, la2TNotFoundTunnelsCount)

	// Check that ny5 has 1 "tunnel not found" gauge metric value, since it has a link with a non-existent device.
	log.Debug("==> Checking that ny5 has 1 not found tunnels")
	ny5NotFoundTunnelsGaugeValues := ny5MetricsClient.GetGaugeValues("doublezero_device_telemetry_agent_peer_discovery_not_found_tunnels")
	require.NotNil(t, ny5NotFoundTunnelsGaugeValues)
	require.Equal(t, 1, len(ny5NotFoundTunnelsGaugeValues))
	require.Contains(t, ny5NotFoundTunnelsGaugeValues[0].Labels, "local_device_pk")
	require.Equal(t, ny5DevicePK.String(), ny5NotFoundTunnelsGaugeValues[0].Labels["local_device_pk"])
	ny5TNotFoundTunnelsCount := int(ny5NotFoundTunnelsGaugeValues[0].Value)
	require.Equal(t, 1, ny5TNotFoundTunnelsCount)

	// Check that the "errors_total" counter has not increased from startup.
	log.Debug("==> Checking that errors_total counter has not increased from startup")
	la2ErrorsCounterValues = la2MetricsClient.GetCounterValues("doublezero_device_telemetry_agent_errors_total")
	if la2ErrorsCounterValues != nil {
		require.Equal(t, prevLA2ErrorsCount, int(la2ErrorsCounterValues[0].Value), "la2 errors_total should be 0: %v", la2ErrorsCounterValues)
	}
	ny5ErrorsCounterValues = ny5MetricsClient.GetCounterValues("doublezero_device_telemetry_agent_errors_total")
	if ny5ErrorsCounterValues != nil {
		require.Equal(t, prevNY5ErrorsCount, int(ny5ErrorsCounterValues[0].Value), "ny5 errors_total should be 0: %v", ny5ErrorsCounterValues)
	}

	// Check that go_memstats_alloc_bytes gauge is less than 10MB.
	log.Debug("==> Checking that go_memstats_alloc_bytes gauge is less than 10MB on both devices")
	la2MemStatsAllocBytes := la2MetricsClient.GetGaugeValues(prometheus.MetricNameGoMemstatsAllocBytes)
	require.NotNil(t, la2MemStatsAllocBytes)
	require.Less(t, int(la2MemStatsAllocBytes[0].Value), int(10*1024*1024))
	ny5MemStatsAllocBytes := ny5MetricsClient.GetGaugeValues(prometheus.MetricNameGoMemstatsAllocBytes)
	require.NotNil(t, ny5MemStatsAllocBytes)
	require.Less(t, int(ny5MemStatsAllocBytes[0].Value), int(10*1024*1024))

	// Check that go_goroutines gauge is less than 20.
	log.Debug("==> Checking that go_goroutines gauge is less than 30 on both devices")
	la2GoGoroutinesCounterValues := la2MetricsClient.GetGaugeValues(prometheus.MetricNameGoGoroutines)
	require.NotNil(t, la2GoGoroutinesCounterValues)
	require.Less(t, int(la2GoGoroutinesCounterValues[0].Value), 30)
	ny5GoGoroutinesCounterValues := ny5MetricsClient.GetGaugeValues(prometheus.MetricNameGoGoroutines)
	require.NotNil(t, ny5GoGoroutinesCounterValues)
	require.Less(t, int(ny5GoGoroutinesCounterValues[0].Value), 30)
}

func waitForDeviceLatencySamples(t *testing.T, dn *devnet.Devnet, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, waitForMinSamples int, waitForNonZeroSample bool, timeout time.Duration) (*telemetrysdk.DeviceLatencySamples, time.Duration) {
	client, err := dn.Ledger.GetTelemetryClient(nil)
	require.NoError(t, err)

	var lastErr error
	var lastAccount *telemetrysdk.DeviceLatencySamples

	start := time.Now()
	if !assert.Eventually(t, func() bool {
		account, err := client.GetDeviceLatencySamples(t.Context(), originDevicePK, targetDevicePK, linkPK, epoch)
		lastErr = err
		lastAccount = account
		if err != nil && !errors.Is(err, telemetrysdk.ErrAccountNotFound) {
			t.Fatalf("failed to get device latency samples: %v", err)
		}
		if account == nil || len(account.Samples) <= waitForMinSamples {
			return false
		}
		if waitForNonZeroSample {
			for _, rtt := range account.Samples {
				if rtt > 0 {
					return true
				}
			}
			return false
		}
		return true
	}, timeout, 3*time.Second) {
		t.Fatalf("waitForDeviceLatencySamples timed out after %s: origin=%s target=%s link=%s epoch=%d waitForMinSamples=%d waitForNonZeroSample=%v lastErr=%v lastAccount=%+v",
			timeout, originDevicePK, targetDevicePK, linkPK, epoch, waitForMinSamples, waitForNonZeroSample, lastErr, lastAccount)
	}

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

func bytesToIP4Net(b [5]byte) *net.IPNet {
	ip := net.IPv4(b[0], b[1], b[2], b[3])
	mask := net.CIDRMask(int(b[4]), 32)
	return &net.IPNet{IP: ip.To4(), Mask: mask}
}
