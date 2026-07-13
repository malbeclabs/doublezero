//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// TestE2E_Feed_CRUD exercises the `doublezero feed` CLI lifecycle (create → get → list → update →
// delete) against a live devnet.
//
// A feed is scoped to a single metro (exchange) and references multicast groups by pubkey.
// CreateFeed stores those pubkeys without requiring the group accounts to exist, so the test uses a
// real exchange created during devnet init (xlax) and synthetic group pubkeys.
func TestE2E_Feed_CRUD(t *testing.T) {
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
	require.NoError(t, dn.Start(ctx, nil))

	// run executes a doublezero CLI command in the manager container, failing the test on error.
	run := func(script string) []byte {
		t.Helper()
		out, err := dn.Manager.Exec(ctx, []string{"bash", "-c", "set -euo pipefail; " + script})
		require.NoError(t, err, "command failed: %s\noutput: %s", script, string(out))
		return out
	}

	type feedJSON struct {
		Account  string `json:"account"`
		Code     string `json:"code"`
		Name     string `json:"name"`
		Exchange string `json:"exchange"`
		Groups   int    `json:"groups"`
	}

	// Resolve a real exchange pubkey created during devnet init.
	var ex struct {
		Account string `json:"account"`
	}
	require.NoError(t, json.Unmarshal(run("doublezero exchange get --code xlax --json"), &ex))
	require.NotEmpty(t, ex.Account, "expected an xlax exchange pubkey")

	group1 := solana.NewWallet().PublicKey().String()
	group2 := solana.NewWallet().PublicKey().String()
	group3 := solana.NewWallet().PublicKey().String()

	// Create a feed serving the xlax metro with two groups. The exchange is passed by code to
	// exercise CLI code→pubkey resolution; the get below asserts it resolved to ex.Account.
	run(fmt.Sprintf(
		`doublezero feed create --code shreds-lax --name "Shreds LAX" --exchange xlax --group %s --group %s`,
		group1, group2,
	))

	// Get and verify the created feed.
	var feed feedJSON
	require.NoError(t, json.Unmarshal(run("doublezero feed get --pubkey shreds-lax --json"), &feed))
	require.NotEmpty(t, feed.Account)
	require.Equal(t, "shreds-lax", feed.Code)
	require.Equal(t, "Shreds LAX", feed.Name)
	require.Equal(t, ex.Account, feed.Exchange)
	require.Equal(t, 2, feed.Groups)

	// List and verify it appears.
	var feeds []feedJSON
	require.NoError(t, json.Unmarshal(run("doublezero feed list --json"), &feeds))
	require.True(t,
		slices.ContainsFunc(feeds, func(f feedJSON) bool { return f.Code == "shreds-lax" }),
		"created feed should appear in feed list",
	)

	// Update the name and replace the group set with a single group.
	run(fmt.Sprintf(`doublezero feed update --pubkey shreds-lax --name "Shreds LAX v2" --group %s`, group3))
	require.NoError(t, json.Unmarshal(run("doublezero feed get --pubkey shreds-lax --json"), &feed))
	require.Equal(t, "Shreds LAX v2", feed.Name)
	require.Equal(t, ex.Account, feed.Exchange, "exchange is immutable across updates")
	require.Equal(t, 1, feed.Groups)

	// Delete and verify it's gone from the list.
	run("doublezero feed delete --pubkey shreds-lax")
	require.NoError(t, json.Unmarshal(run("doublezero feed list --json"), &feeds))
	require.False(t,
		slices.ContainsFunc(feeds, func(f feedJSON) bool { return f.Code == "shreds-lax" }),
		"deleted feed should no longer appear in feed list",
	)
}
