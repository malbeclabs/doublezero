//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// TestE2E_Feed_CRUD exercises the `doublezero feed` CLI lifecycle (create → get → list → update →
// delete) against a live devnet.
//
// A feed is scoped to a single metro (exchange) and references multicast groups. The CLI reads back
// both the metro and every group, so the test uses the xlax exchange that devnet init creates and
// three multicast groups it creates first.
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

	for _, group := range []string{"feed-mc01", "feed-mc02", "feed-mc03"} {
		run(fmt.Sprintf("doublezero multicast group create --code %s --max-bandwidth 1Gbps --owner me -w", group))
	}

	// Create a feed serving the xlax metro with two groups.
	run(`doublezero feed create --code shreds-lax --name "Shreds LAX" --exchange xlax --group feed-mc01 --group feed-mc02`)

	// getFeed narrows the list to the one feed with this code in the xlax metro.
	getFeed := func() feedJSON {
		var rows []feedJSON
		require.NoError(t, json.Unmarshal(
			run("doublezero feed list --code shreds-lax --exchange xlax --json"), &rows))
		require.Len(t, rows, 1, "expected exactly one shreds-lax feed in xlax")
		return rows[0]
	}

	// Read back and verify the created feed.
	feed := getFeed()
	require.NotEmpty(t, feed.Account)
	require.Equal(t, "shreds-lax", feed.Code)
	require.Equal(t, "Shreds LAX", feed.Name)
	require.Equal(t, "xlax", feed.Exchange)
	require.Equal(t, 2, feed.Groups)

	// List and verify it appears.
	var feeds []feedJSON
	require.NoError(t, json.Unmarshal(run("doublezero feed list --json"), &feeds))
	require.True(t,
		slices.ContainsFunc(feeds, func(f feedJSON) bool { return f.Code == "shreds-lax" }),
		"created feed should appear in feed list",
	)

	// Update the name and replace the group set with a single group.
	run(fmt.Sprintf(`doublezero feed update --pubkey %s --name "Shreds LAX v2" --group feed-mc03`, feed.Account))
	feed = getFeed()
	require.Equal(t, "Shreds LAX v2", feed.Name)
	require.Equal(t, "xlax", feed.Exchange, "exchange is immutable across updates")
	require.Equal(t, 1, feed.Groups)

	// Delete and verify it's gone from the list.
	run(fmt.Sprintf("doublezero feed delete --pubkey %s", feed.Account))
	require.NoError(t, json.Unmarshal(run("doublezero feed list --json"), &feeds))
	require.False(t,
		slices.ContainsFunc(feeds, func(f feedJSON) bool { return f.Code == "shreds-lax" }),
		"deleted feed should no longer appear in feed list",
	)
}
