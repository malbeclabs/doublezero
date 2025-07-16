//go:build e2e

package e2e_test

import (
	    "fmt"
        "os"
        "path/filepath"
        "strconv"
        "strings"
        "testing"

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
            DeployID: deployID,
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
    	initOutput, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
    		set -euo pipefail

    		echo "==> Fetching initial global configuration onchain"
    		doublezero global-config get
    	`})
        require.NoError(t, err, "error fetching initial global config")

        remoteAsn, err := ParseValuesFromOutput(strings.SplitAfter(string(initOutput), "\n"), "remote asn", 1)
        require.NoError(t, err, "error fetching initial remote asn from output")

        remoteAsnInt, err := strconv.Atoi(remoteAsn)
        require.NoError(t, err, "error parsing initial remote asn")

        newAsn := remoteAsnInt + 100
        finalOutput, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
        	set -euo pipefail

            echo "==> Updating global configuration onchain"
    		doublezero global-config set --remote-asn 75342
    		echo "--> Global configuration onchain:"
    		doublezero global-config get
        `})
        require.NoError(t, err, "error updating global config and fetching response")

        newAsnOut, err := ParseValuesFromOutput(strings.SplitAfter(string(finalOutput), "\n"), "remote asn", 1)
        require.NoError(t, err, "error fextching new remote asn from output")

        newAsnInt, err := strconv.Atoi(newAsnOut)
        require.NoError(t, err, "error parsing new remote asn from output")

        require.Equal(t, newAsn, newAsnInt, "expected remote asn updated to: %d, got %d\n", newAsn, newAsnInt)
    })
}

func ParseValuesFromOutput(lines []string, columnName string, rowIdx int) (string, error) {
	headers := strings.Fields(lines[0])
	if rowIdx >= len(lines) - 1 {
		return "", fmt.Errorf("no such row %d", rowIdx)
	}
	data := strings.Fields(lines[rowIdx+1])
	for idx, header := range headers {
		if header == columnName {
			if idx >= len(data) {
				return "", fmt.Errorf("column index %d out of range", idx)
			}
			return data[idx], nil
		}
	}
	return "", fmt.Errorf("column %s not found", columnName)
} 
