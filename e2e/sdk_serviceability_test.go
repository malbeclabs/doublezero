//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

func TestE2E_SDK_Serviceability(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

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

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	t.Run("update global config", func(t *testing.T) {
		client, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err, "error getting serviceability program client")

		err = client.Load(ctx)
		require.NoError(t, err, "error loading accounts into context")

		config := client.GetConfig()

		initialLocalAsn := config.Local_asn
		initialRemoteAsn := config.Remote_asn
		initialDeviceBlock := config.TunnelTunnelBlock
		initialUserBlock := config.UserTunnelBlock
		initialMulticastBlock := config.MulticastGroupBlock
		newAsn := initialRemoteAsn + 100

		_, err = dn.Manager.Exec(ctx, []string{"doublezero", "global-config", "set", "--remote-asn", strconv.Itoa(int(newAsn))})
		require.NoError(t, err, "error setting global config value")

		require.Eventually(t, func() bool {
			err := client.Load(ctx)
			require.NoError(t, err, "error while reloading onchain state to verify update")

			config = client.GetConfig()
			return newAsn == config.Remote_asn && initialLocalAsn == config.Local_asn && initialDeviceBlock == config.TunnelTunnelBlock && initialUserBlock == config.UserTunnelBlock && initialMulticastBlock == config.MulticastGroupBlock
		}, 30*time.Second, 1*time.Second)
	})
}
