//go:build e2e

package e2e_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/stretchr/testify/require"
)

func TestE2E_DeviceAgentTelemetry(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)
	workingDir := t.TempDir()

	// Generate a program keypair.
	programKeypairPath := filepath.Join(workingDir, "dz-program-keypair.json")
	err := solana.GenerateKeypair(programKeypairPath)
	if err != nil {
		t.Fatal("failed to generate program keypair")
	}

	// Generate manager keypair.
	managerKeypairPath := filepath.Join(workingDir, "dz-manager-keypair.json")
	err = solana.GenerateKeypair(managerKeypairPath)
	if err != nil {
		t.Fatal("failed to generate manager keypair")
	}

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:   deployID,
		WorkingDir: workingDir,

		CYOANetworkSpec: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Ledger: devnet.LedgerSpec{
			ProgramKeypairPath: programKeypairPath,
		},
		Manager: devnet.ManagerSpec{
			KeypairPath: managerKeypairPath,
		},
		Controller: devnet.ControllerSpec{
			CYOANetworkIPHostID: 99,
		},
		Activator: devnet.ActivatorSpec{},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	log.Info("==> Starting devnet")
	err = dn.Start(context.Background())
	require.NoError(t, err)

	// Add and start the 2 devices in parallel.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()

		// Add the la2-dz01 device.
		_, err = dn.AddDevice(context.Background(), devnet.DeviceSpec{
			Code: "la2-dz01",
			// .8/29 has network address .8, allocatable up to .14, and broadcast .15
			CYOANetworkIPHostID:          8,
			CYOANetworkAllocatablePrefix: 29,
		})
		require.NoError(t, err)
	}()
	go func() {
		defer wg.Done()

		// Add the ny5-dz01 device.
		_, err = dn.AddDevice(context.Background(), devnet.DeviceSpec{
			Code: "ny5-dz01",
			// .16/29 has network address .16, allocatable up to .22, and broadcast .23
			CYOANetworkIPHostID:          16,
			CYOANetworkAllocatablePrefix: 29,
		})
		require.NoError(t, err)
	}()
	wg.Wait()

	// Add some dummy devices onchain.
	log.Info("==> Adding dummy devices onchain")
	_, err = dn.Manager.Exec(context.Background(), []string{"bash", "-c", `
			set -e

			doublezero device create --code ld4-dz01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.72/29"
			doublezero device create --code frk-dz01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.88/29"
			doublezero device create --code sg1-dz01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "180.87.102.104/29"
			doublezero device create --code ty2-dz01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "180.87.154.112/29"
			doublezero device create --code pit-dzd01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32"
			doublezero device create --code ams-dz001 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "195.219.138.56/29"
	`})
	require.NoError(t, err)

	// Add links onchain, including our real devices and some devices that haven't been added yet.
	log.Info("==> Creating links onchain")
	_, err = dn.Manager.Exec(context.Background(), []string{"bash", "-c", `
			set -e

			doublezero link create --code "la2-dz01:ny5-dz01" --side-a la2-dz01 --side-z ny5-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3
			doublezero link create --code "ny5-dz01:ld4-dz01" --side-a ny5-dz01 --side-z ld4-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3
			doublezero link create --code "ld4-dz01:frk-dz01" --side-a ld4-dz01 --side-z frk-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10
			doublezero link create --code "ld4-dz01:sg1-dz01" --side-a ld4-dz01 --side-z sg1-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9
			doublezero link create --code "sg1-dz01:ty2-dz01" --side-a sg1-dz01 --side-z ty2-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7
			doublezero link create --code "ty2-dz01:la2-dz01" --side-a ty2-dz01 --side-z la2-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 10
		`})
	require.NoError(t, err)

	// Check that the devices are reachable from each other via ping.
	log.Info("==> Checking that devices are reachable from each other via ping")
	_, err = dn.Devices[0].Exec(context.Background(), []string{"ping", "-c", "1", dn.Devices[1].CYOANetworkIP})
	require.NoError(t, err)
	_, err = dn.Devices[1].Exec(context.Background(), []string{"ping", "-c", "1", dn.Devices[0].CYOANetworkIP})
	require.NoError(t, err)

	// Check that TWAMP probes work between the devices.
	// TODO(snormore): Remove this when we have agent telemetry implemented, and check that instead.
	log.Info("==> Checking that TWAMP probes work between the devices")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sender := dn.Devices[1]
	reflector := dn.Devices[0]
	go func() {
		_, err = reflector.Exec(ctx, []string{"twamp-reflector"})
		require.NoError(t, err)
	}()
	require.Eventually(t, func() bool {
		_, err := reflector.Exec(t.Context(), []string{"bash", "-c", "ss -uln '( dport = :862 )' | grep -q ."})
		return err == nil
	}, 3*time.Second, 100*time.Millisecond)
	output, err := sender.Exec(t.Context(), []string{"twamp-sender", reflector.CYOANetworkIP})
	require.NoError(t, err)
	log.Info("TWAMP sender output", "output", string(output))
	require.Contains(t, string(output), "RTT:")
	rtt, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(string(output), "RTT: ")))
	require.NoError(t, err)
	require.Greater(t, rtt, 0*time.Millisecond)
}
