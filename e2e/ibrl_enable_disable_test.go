//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/fixtures"
	"github.com/stretchr/testify/require"

	dockercontainer "github.com/docker/docker/api/types/container"
)

func TestE2E_IBRL_EnableDisable(t *testing.T) {
	t.Parallel()

	dn, device, client := NewSingleDeviceSingleClientTestDevnet(t)

	// Step 1: Connect IBRL — this implicitly enables the reconciler.
	if !t.Run("connect", func(t *testing.T) {
		dn.ConnectIBRLUserTunnel(t, client)
		err := client.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		checkIBRLPostConnect(t, dn, device, client)
	}) {
		t.Fail()
		return
	}

	// Step 2: Verify reconciler is enabled via v2/status.
	if !t.Run("reconciler_enabled_after_connect", func(t *testing.T) {
		reconcilerEnabled, services := getV2Status(t, client)
		require.True(t, reconcilerEnabled, "reconciler should be enabled after connect")
		require.NotEmpty(t, services, "should have services after connect")
	}) {
		t.Fail()
		return
	}

	// Step 3: Disable reconciler — should tear down tunnels.
	if !t.Run("disable", func(t *testing.T) {
		_, err := client.Exec(t.Context(), []string{"doublezero", "disable"})
		require.NoError(t, err)

		// Wait for tunnel to be torn down. After disable, the reconciler tears
		// down all tunnels so the status array becomes empty. Poll the v2/status
		// endpoint until services is empty.
		waitForReconcilerState(t, client, false, 60*time.Second)
	}) {
		t.Fail()
		return
	}

	// Step 4: Verify status shows reconciler disabled with no tunnel.
	if !t.Run("status_after_disable", func(t *testing.T) {
		// The CLI `doublezero status` should show no tunnel rows.
		got, err := client.Exec(t.Context(), []string{"doublezero", "status"})
		require.NoError(t, err)

		want, err := fixtures.Render("fixtures/ibrl/doublezero_status_disconnected.tmpl", map[string]any{"Reconciler": "false"})
		require.NoError(t, err)

		diff := fixtures.DiffCLITable(got, []byte(want))
		if diff != "" {
			fmt.Println(string(got))
			t.Fatalf("output mismatch: -(want), +(got):%s", diff)
		}

		// Tunnel interface should not exist.
		gotIface, err := client.Exec(t.Context(), []string{"bash", "-c", "ip -j link show dev doublezero0"}, docker.NoPrintOnError())
		require.Error(t, err)
		require.Equal(t, `Device "doublezero0" does not exist.`, strings.TrimSpace(string(gotIface)))
	}) {
		t.Fail()
		return
	}

	// Step 5: Re-enable reconciler — should reprovision from onchain state.
	if !t.Run("enable", func(t *testing.T) {
		_, err := client.Exec(t.Context(), []string{"doublezero", "enable"})
		require.NoError(t, err)

		// The reconciler should reprovision the tunnel from onchain state.
		err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)
	}) {
		t.Fail()
		return
	}

	// Step 6: Verify status shows reconciler enabled with tunnel up.
	if !t.Run("status_after_enable", func(t *testing.T) {
		checkIBRLPostConnect(t, dn, device, client)
	}) {
		t.Fail()
		return
	}

	// Step 7: Restart daemon container — reconciler state should persist.
	if !t.Run("daemon_restart_persists_state", func(t *testing.T) {
		// Restart the client container (which runs the daemon).
		timeout := 10 // seconds
		err := dockerClient.ContainerRestart(t.Context(), client.ContainerID, dockercontainer.StopOptions{
			Timeout: &timeout,
		})
		require.NoError(t, err)

		// Wait for the daemon to be ready after restart.
		err = client.WaitForDaemonReady(t.Context(), 60*time.Second)
		require.NoError(t, err)

		// The reconciler should resume from the persisted enabled state and
		// reprovision the tunnel.
		err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		// Verify status is correct.
		reconcilerEnabled, services := getV2Status(t, client)
		require.True(t, reconcilerEnabled, "reconciler should be enabled after restart")
		require.NotEmpty(t, services, "should have services after restart")
	}) {
		t.Fail()
		return
	}

	// Step 8: Disconnect — clean teardown.
	if !t.Run("disconnect", func(t *testing.T) {
		dn.DisconnectUserTunnel(t, client)

		checkIBRLPostDisconnect(t, dn, device, client)
	}) {
		t.Fail()
	}
}

// getV2Status queries the daemon's /v2/status endpoint and returns the
// reconciler_enabled flag and services list.
func getV2Status(t *testing.T, client *devnet.Client) (bool, []any) {
	t.Helper()

	output, err := client.Exec(t.Context(), []string{
		"curl", "-s", "--unix-socket", "/var/run/doublezerod/doublezerod.sock", "http://doublezero/v2/status",
	})
	require.NoError(t, err, "failed to query v2/status")

	var resp map[string]any
	err = json.Unmarshal(output, &resp)
	require.NoError(t, err, "failed to parse v2/status response: %s", string(output))

	enabled, ok := resp["reconciler_enabled"].(bool)
	require.True(t, ok, "reconciler_enabled should be a bool in: %s", string(output))

	services, _ := resp["services"].([]any)
	return enabled, services
}

// waitForReconcilerState polls the v2/status endpoint until the reconciler
// reaches the expected enabled state and (when disabled) services are empty.
func waitForReconcilerState(t *testing.T, client *devnet.Client, wantEnabled bool, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output, err := client.Exec(t.Context(), []string{
			"curl", "-s", "--unix-socket", "/var/run/doublezerod/doublezerod.sock", "http://doublezero/v2/status",
		}, docker.NoPrintOnError())
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		var resp map[string]any
		if err := json.Unmarshal(output, &resp); err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		enabled, _ := resp["reconciler_enabled"].(bool)
		services, _ := resp["services"].([]any)

		if enabled == wantEnabled {
			if wantEnabled || len(services) == 0 {
				return
			}
		}

		time.Sleep(1 * time.Second)
	}

	t.Fatalf("timed out waiting for reconciler_enabled=%v (timeout %s)", wantEnabled, timeout)
}
