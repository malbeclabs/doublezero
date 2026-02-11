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

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	t.Run("update global config", func(t *testing.T) {
		client, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err, "error getting serviceability program client")

		data, err := client.GetProgramData(ctx)
		require.NoError(t, err, "error loading accounts into context")

		config := data.GlobalConfig

		newAsn := config.RemoteASN + 100

		_, err = dn.Manager.Exec(ctx, []string{"doublezero", "global-config", "set", "--remote-asn", strconv.Itoa(int(newAsn))})
		require.NoError(t, err, "error setting global config value")

		require.Eventually(t, func() bool {
			data, err := client.GetProgramData(ctx)
			require.NoError(t, err, "error while reloading onchain state to verify update")

			got := data.GlobalConfig.RemoteASN
			if got == newAsn {
				return true
			}

			log.Debug("--> Waiting for global config update", "want", newAsn, "got", got)
			return false
		}, 30*time.Second, 3*time.Second)
	})
}
