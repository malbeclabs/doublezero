//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// KNOWN INCOMPATIBILITIES - Review and remove as versions are deprecated
// =============================================================================
//
// This map lists CLI commands that are known to be incompatible with the current
// onchain program for versions older than the specified version. These failures
// are expected and will be reported as "KNOWN_FAIL" instead of "FAIL".
//
// Format: "step_name" -> "minimum_compatible_version"
//
// When adding entries:
//   - Document WHY the incompatibility exists
//   - Set the version to the first CLI version that IS compatible
//   - Remove entries when min_compatible_version is bumped past them
//
var knownIncompatibilities = map[string]string{
	// multicast_group_create: The MulticastGroupCreateArgs Borsh struct changed in v0.8.1.
	// The index and bump_seed fields were removed. Older CLIs send the old format which
	// causes Borsh deserialization failure in the current program.
	"write/multicast_group_create": "0.8.1",
}

// =============================================================================
// PER-ENVIRONMENT CONFIG - Features that vary by environment
// =============================================================================
//
// Some features are only enabled on certain environments. This config controls
// environment-specific behavior for the compatibility test.

type compatEnvConfig struct {
	OnchainAllocation bool // Whether to use onchain resource allocation
}

var compatEnvConfigs = map[string]compatEnvConfig{
	"devnet":      {OnchainAllocation: true},
	"testnet":     {OnchainAllocation: true},
	"mainnet-beta": {OnchainAllocation: false}, // Not yet enabled on mainnet
}

// isKnownIncompatible checks if a step failure is expected for the given CLI version.
// Returns true if the step has a known incompatibility and the version is older than
// the minimum compatible version for that step.
func isKnownIncompatible(stepName, cliVersion string) bool {
	minCompatVersion, exists := knownIncompatibilities[stepName]
	if !exists {
		return false
	}
	cliVer, ok := devnet.ParseSemver(cliVersion)
	if !ok {
		return false
	}
	minVer, ok := devnet.ParseSemver(minCompatVersion)
	if !ok {
		return false
	}
	return devnet.CompareProgramVersions(cliVer, minVer) < 0
}

// TestE2E_BackwardCompatibility tests that older CLI versions can still interact
// with the upgraded onchain program. This validates that Borsh-serialized instructions
// from older CLIs are correctly deserialized by the current program binary.
//
// Architecture:
//   - Ledger: solana-test-validator with cloned accounts + upgraded program .so
//   - Manager: container with current CLI + old CLI versions installed via Cloudsmith apt
//   - Activator: watches for onchain events and processes status transitions
//   - Controller: pushes configs to devices (not exercised)
//
// Test flow:
//  1. Clone program accounts from a remote cluster into a local test validator
//  2. Deploy the current branch's upgraded program .so over the cloned program ID
//  3. Patch GlobalState to add the test manager to the foundation_allowlist (write auth)
//  4. Read ProgramConfig.MinCompatVersion from onchain state
//  5. Install each compatible CLI version (min through current-1) from Cloudsmith
//  6. For each version, run read and write workflows against the upgraded program
//
// Environment variables:
//   - DZ_COMPAT_CLONE_ENV: comma-separated environments to test (default: "testnet,mainnet-beta")
//   - DZ_COMPAT_MIN_VERSION: override ProgramConfig.MinCompatVersion (e.g., "0.8.1")
//   - DZ_COMPAT_MAX_NUM_VERSIONS: limit number of versions to test (0 = all, e.g., "2")
// compatStepResult tracks the result of a single step for a single version.
type compatStepResult struct {
	name   string
	status string // "PASS", "FAIL", "SKIP", "KNOWN_FAIL"
	err    string // error message if failed
}

// compatEnvResults holds the compatibility matrix results for a single environment.
type compatEnvResults struct {
	env      string
	versions []string                  // ordered list of versions tested
	matrix   map[string][]compatStepResult // version -> results
	mu       sync.Mutex
}

func (r *compatEnvResults) record(version, name, status, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.matrix[version] = append(r.matrix[version], compatStepResult{name, status, errMsg})
}

// formatMatrix renders the summary and detail matrices for this environment.
func (r *compatEnvResults) formatMatrix() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("\n=== %s: Compatibility Matrix (Summary) ===\n\n", r.env))

	for _, version := range r.versions {
		results := r.matrix[version]
		if len(results) == 0 {
			continue
		}
		var passed, failed, knownFail, skipped int
		for _, res := range results {
			switch res.status {
			case "PASS":
				passed++
			case "FAIL":
				failed++
			case "KNOWN_FAIL":
				knownFail++
			case "SKIP":
				skipped++
			}
		}
		if failed == 0 {
			buf.WriteString(fmt.Sprintf("v%-10s  ALL PASSED (%d passed", version, passed))
			if knownFail > 0 {
				buf.WriteString(fmt.Sprintf(", %d known incompatible", knownFail))
			}
			if skipped > 0 {
				buf.WriteString(fmt.Sprintf(", %d skipped", skipped))
			}
			buf.WriteString(")\n")
		} else {
			buf.WriteString(fmt.Sprintf("v%-10s  %d passed, %d FAILED", version, passed, failed))
			if knownFail > 0 {
				buf.WriteString(fmt.Sprintf(", %d known incompatible", knownFail))
			}
			if skipped > 0 {
				buf.WriteString(fmt.Sprintf(", %d skipped", skipped))
			}
			buf.WriteString("\n")
		}
	}

	buf.WriteString(fmt.Sprintf("\n=== %s: Compatibility Matrix (Detail) ===\n\n", r.env))

	// Collect unique step names in order of first appearance.
	var stepNames []string
	seen := make(map[string]bool)
	for _, version := range r.versions {
		for _, res := range r.matrix[version] {
			if !seen[res.name] {
				seen[res.name] = true
				stepNames = append(stepNames, res.name)
			}
		}
	}

	maxNameLen := 0
	for _, name := range stepNames {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
	}

	// Header row.
	buf.WriteString(fmt.Sprintf("%-*s", maxNameLen+2, ""))
	for _, version := range r.versions {
		buf.WriteString(fmt.Sprintf("  v%-8s", version))
	}
	buf.WriteString("\n")

	// Build lookup.
	lookup := make(map[string]map[string]compatStepResult)
	for _, version := range r.versions {
		lookup[version] = make(map[string]compatStepResult)
		for _, res := range r.matrix[version] {
			lookup[version][res.name] = res
		}
	}

	for _, name := range stepNames {
		buf.WriteString(fmt.Sprintf("%-*s", maxNameLen+2, name))
		for _, version := range r.versions {
			res, ok := lookup[version][name]
			if !ok {
				buf.WriteString(fmt.Sprintf("  %-10s", "-"))
			} else {
				buf.WriteString(fmt.Sprintf("  %-10s", res.status))
			}
		}
		buf.WriteString("\n")
	}

	return buf.String()
}

func TestE2E_BackwardCompatibility(t *testing.T) {
	t.Parallel()

	// Parse comma-separated environments. Defaults to testnet + mainnet-beta.
	cloneEnvs := os.Getenv("DZ_COMPAT_CLONE_ENV")
	if cloneEnvs == "" {
		cloneEnvs = "testnet,mainnet-beta"
	}
	var envList []string
	for _, env := range strings.Split(cloneEnvs, ",") {
		env = strings.TrimSpace(env)
		if env != "" {
			envList = append(envList, env)
		}
	}

	// Collect results from all environments for a combined summary at the end.
	var (
		allResultsMu sync.Mutex
		allResults   []*compatEnvResults
	)

	for _, cloneEnv := range envList {
		cloneEnv := cloneEnv
		t.Run(cloneEnv, func(t *testing.T) {
			t.Parallel()
			envResults := &compatEnvResults{
				env:    cloneEnv,
				matrix: make(map[string][]compatStepResult),
			}
			allResultsMu.Lock()
			allResults = append(allResults, envResults)
			allResultsMu.Unlock()

			testBackwardCompatibilityForEnv(t, cloneEnv, envResults)
		})
	}

	// Print combined compatibility matrices after all env sub-tests complete.
	t.Cleanup(func() {
		allResultsMu.Lock()
		defer allResultsMu.Unlock()

		var buf strings.Builder
		buf.WriteString("\n\n========================================")
		buf.WriteString("\n  Combined Compatibility Results")
		buf.WriteString("\n========================================\n")
		for _, er := range allResults {
			buf.WriteString(er.formatMatrix())
		}
		logger.Info(buf.String())
	})
}

func testBackwardCompatibilityForEnv(t *testing.T, cloneEnv string, envResults *compatEnvResults) {
	deployID := "dz-e2e-BackwardCompat-" + cloneEnv + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	programID, ok := serviceability.ProgramIDs[cloneEnv]
	require.True(t, ok, "unknown environment %q (valid: mainnet-beta, testnet, devnet)", cloneEnv)
	rpcURL, ok := serviceability.LedgerRPCURLs[cloneEnv]
	require.True(t, ok, "no RPC URL for environment %q", cloneEnv)
	log.Info("==> Cloning state from environment", "env", cloneEnv, "programID", programID)

	// Create the devnet. The manager keypair is auto-generated in New().
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),
		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Ledger: devnet.LedgerSpec{
			CloneFromURL:    rpcURL,
			CloneProgramIDs: []string{programID},
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
			ServiceabilityProgramID:          programID,
		},
		// Use per-environment config for onchain allocation. Mainnet doesn't have
		// onchain allocation enabled yet, so we use legacy allocation there.
		Activator: devnet.ActivatorSpec{
			OnchainAllocation: devnet.BoolPtr(compatEnvConfigs[cloneEnv].OnchainAllocation),
		},
		SkipProgramDeploy: true,
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	// Read the manager pubkey from the auto-generated keypair so we can set it
	// as the upgrade authority for the cloned program.
	managerKeypairJSON, err := os.ReadFile(dn.Spec.Manager.ManagerKeypairPath)
	require.NoError(t, err)
	managerPubkey, err := solana.PubkeyFromKeypairJSON(managerKeypairJSON)
	require.NoError(t, err)

	// Configure the ledger to deploy the upgraded program at startup.
	dn.Spec.Ledger.UpgradeProgramID = programID
	dn.Spec.Ledger.UpgradeProgramSOPath = devnet.UpgradeProgramContainerSOPath
	dn.Spec.Ledger.UpgradeAuthority = managerPubkey
	dn.Spec.Ledger.PatchGlobalStateAuthority = managerPubkey

	log.Info("==> Starting devnet with cloned state and upgraded program",
		"programID", programID,
		"upgradeAuthority", managerPubkey,
	)

	// Start components individually instead of dn.Start() so we can initialize the smart
	// contract before starting the activator. The activator needs ProgramConfig and
	// GlobalConfig PDAs to exist, which are created by `doublezero init`. With
	// SkipProgramDeploy, init doesn't happen inside dn.Start(), so we do it manually
	// between starting the manager and the activator.
	_, err = dn.DefaultNetwork.CreateIfNotExists(t.Context())
	require.NoError(t, err)
	_, err = dn.Ledger.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	_, err = dn.Manager.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	_, err = dn.Funder.StartIfNotRunning(t.Context())
	require.NoError(t, err)

	// Configure the manager CLI to use the cloned program ID. Older CLI versions read
	// the program ID from this config (they don't support env var overrides), and the
	// program ID affects PDA derivation for commands like global-config get.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		fmt.Sprintf("doublezero config set --program-id %s", programID)})
	require.NoError(t, err)

	// Fund the manager account since we skipped program deployment which normally does this.
	_, err = dn.Manager.Exec(t.Context(), []string{"solana", "airdrop", "100"})
	require.NoError(t, err)

	// Initialize the smart contract. This creates ProgramConfig, GlobalState, and other
	// PDA accounts that the upgraded program needs but may not exist on the cloned cluster yet.
	log.Info("==> Initializing smart contract")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero init"})
	require.NoError(t, err)

	// Now start the activator and controller — they need the PDAs to exist.
	_, err = dn.Controller.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	_, err = dn.Activator.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	log.Info("--> Devnet started")

	// Read ProgramConfig to get min_compatible_version and current version.
	log.Info("==> Reading ProgramConfig from ledger")

	// The serviceability client needs to use the cloned program ID (not the local one)
	// since we deployed to the cloned program address.
	svcClient, err := devnet.NewServiceabilityClientForProgram(dn, programID)
	require.NoError(t, err)

	programData, err := svcClient.GetProgramData(t.Context())
	require.NoError(t, err)
	require.NotNil(t, programData.ProgramConfig, "ProgramConfig not found onchain")

	minVersion := programData.ProgramConfig.MinCompatVersion
	currentVersion := programData.ProgramConfig.Version

	// DZ_COMPAT_MIN_VERSION overrides ProgramConfig.MinCompatVersion.
	// Useful for testing after bumping the min version: DZ_COMPAT_MIN_VERSION=0.8.1
	if override := os.Getenv("DZ_COMPAT_MIN_VERSION"); override != "" {
		pv, ok := devnet.ParseSemver(override)
		require.True(t, ok, "invalid DZ_COMPAT_MIN_VERSION: %s", override)
		minVersion = pv
	}

	log.Info("--> ProgramConfig",
		"version", devnet.FormatProgramVersion(currentVersion),
		"minCompatVersion", devnet.FormatProgramVersion(minVersion),
	)

	// Set up Cloudsmith repo in the manager container (needed for version enumeration and installs).
	log.Info("==> Setting up Cloudsmith repo in manager container")
	managerExec := func(ctx context.Context, command []string) ([]byte, error) {
		return dn.Manager.Exec(ctx, command)
	}
	err = devnet.SetupCloudsmithRepo(t.Context(), managerExec, cloneEnv)
	require.NoError(t, err)

	// Enumerate available versions from the Cloudsmith apt repo in the manager container.
	aptOutput, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		"apt-cache madison doublezero | awk -F'|' '{print $2}' | sed 's/ //g; s/-1$//' | sort -uV"})
	require.NoError(t, err)
	aptVersions := strings.Split(strings.TrimSpace(string(aptOutput)), "\n")
	// Prefix with "v" for compatibility with EnumerateCompatibleVersions.
	for i, v := range aptVersions {
		if !strings.HasPrefix(v, "v") {
			aptVersions[i] = "v" + v
		}
	}

	compatVersions := devnet.EnumerateCompatibleVersions(aptVersions, minVersion, currentVersion)

	// DZ_COMPAT_MAX_NUM_VERSIONS limits how many versions to test (0 = all).
	// Useful for quick validation: DZ_COMPAT_MAX_NUM_VERSIONS=2
	if maxStr := os.Getenv("DZ_COMPAT_MAX_NUM_VERSIONS"); maxStr != "" {
		var max int
		fmt.Sscanf(maxStr, "%d", &max)
		if max > 0 && max < len(compatVersions) {
			if max == 1 {
				compatVersions = compatVersions[len(compatVersions)-1:]
			} else {
				selected := make([]string, 0, max)
				selected = append(selected, compatVersions[0])
				for i := 1; i < max-1; i++ {
					idx := i * (len(compatVersions) - 1) / (max - 1)
					selected = append(selected, compatVersions[idx])
				}
				selected = append(selected, compatVersions[len(compatVersions)-1])
				compatVersions = selected
			}
		}
	}

	log.Info("--> Compatible versions to test", "versions", compatVersions)

	if len(compatVersions) == 0 {
		t.Skip("no compatible versions found between min_compatible_version and current version")
	}

	// Store the version list in the shared results so the parent can render the matrix.
	envResults.versions = compatVersions

	// Install all old versions in the manager container.
	for _, version := range compatVersions {
		log.Info("==> Installing CLI version in manager", "version", version)
		err = devnet.InstallCLIVersion(t.Context(), managerExec, version)
		require.NoError(t, err)
	}

	recordResult := envResults.record

	// Test each compatible version.
	for vi, version := range compatVersions {
		vi, version := vi, version
		t.Run("v"+version, func(t *testing.T) {
			cli := fmt.Sprintf("doublezero-%s", version)
			log := log.With("version", version, "cli", cli)

			// lookupFirstPubkey returns a bash subshell that extracts the first
			// entity's base58 pubkey from a list command's table output (first column).
			lookupFirstPubkey := func(listCmd string) string {
				return fmt.Sprintf(`$(doublezero %s 2>/dev/null | tail -n +2 | head -1 | awk '{print $1}')`, listCmd)
			}
			// lookupPubkeyByCode returns a bash subshell that finds the pubkey for a
			// specific entity code from the current CLI's table output. The table format
			// has the account pubkey as the first column and the code as the second column.
			lookupPubkeyByCode := func(listCmd, code string) string {
				return fmt.Sprintf("$(doublezero %s 2>/dev/null | grep '%s ' | awk '{print $1}')", listCmd, code)
			}

			// --- Read workflows ---
			// These test that the old CLI can deserialize onchain state written by the
			// current program. Includes both cloned entities and any entities
			// created by earlier version subtests.
			t.Run("manager_read", func(t *testing.T) {
				readCommands := []struct {
					name string
					cmd  string
				}{
					{name: "device_list", cmd: cli + " device list"},
					{name: "link_list", cmd: cli + " link list"},
					{name: "user_list", cmd: cli + " user list"},
					{name: "multicast_group_list", cmd: cli + " multicast group list"},
					{name: "global_config_get", cmd: cli + " global-config get"},
					{name: "location_list", cmd: cli + " location list"},
					{name: "exchange_list", cmd: cli + " exchange list"},
				}

				for _, rc := range readCommands {
					rc := rc
					t.Run(rc.name, func(t *testing.T) {
						t.Parallel()
						stepKey := "read/" + rc.name
						log.Info("==> Running manager read command", "command", rc.cmd)
						output, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", rc.cmd})
						if err == nil {
							recordResult(version, stepKey, "PASS", "")
							log.Info("--> Command succeeded", "command", rc.cmd)
						} else if isKnownIncompatible(stepKey, version) {
							// Known incompatibility - record but don't fail the test
							recordResult(version, stepKey, "KNOWN_FAIL", string(output))
							log.Info("--> Command failed (known incompatibility)", "command", rc.cmd)
						} else {
							// Unexpected failure - fail the test
							assert.NoError(t, err, "command %q failed: %s", rc.cmd, string(output))
							recordResult(version, stepKey, "FAIL", string(output))
						}
					})
				}
			})

			// --- Write workflows (sequential — each step depends on the previous) ---
			//
			// These test that the old CLI's Borsh-serialized instructions are correctly
			// deserialized by the current onchain program. The activator is running and
			// watches for onchain state changes and processes activation.
			//
			// Entity lifecycle tested:
			//   1. Create: contributor, location, exchange, devices, interfaces, link, multicast group
			//   2. Update: location, exchange, contributor, device + cloned entities
			//   3. Read/verify: device list, link list
			//   4. Delete: link (wait for activation first), interfaces, devices
			t.Run("manager_write", func(t *testing.T) {
				suffix := strings.ReplaceAll(version, ".", "")
				contributorCode := "ct" + suffix
				locationCode := "lc" + suffix
				exchangeCode := "ex" + suffix
				deviceCode := "dv" + suffix
				deviceCode2 := "d2" + suffix
				ifaceName := "Ethernet1"
				linkCode := "lk" + suffix
				multicastCode := "mc" + suffix

				type writeStep struct {
					name      string
					cmd       string
					noCascade bool // if true, failure doesn't skip subsequent steps
				}
				writeSteps := []writeStep{
					// --- Phase 1: Create foundational entities ---
					// These are prerequisites for devices. The onchain program creates PDA
					// accounts for each entity, keyed by their code.
					{name: "contributor_create", cmd: cli + " contributor create --code " + contributorCode + " --owner me"},
					{name: "location_create", cmd: cli + " location create --code " + locationCode + " --name TestLoc --country US --lat 40.7 --lng -74.0"},
					{name: "exchange_create", cmd: cli + " exchange create --code " + exchangeCode + " --name TestExchange --lat 40.7 --lng -74.0"},

					// --- Phase 2: Create devices ---
					// Devices are created in "pending" status. The activator processes them
					// and transitions to "device-provisioning". We don't use -w (wait for
					// activation) since we don't need full activation for the test.
					{name: "device_create", cmd: cli + " device create --code " + deviceCode +
						" --contributor " + contributorCode +
						" --location " + locationCode +
						" --exchange " + exchangeCode +
						fmt.Sprintf(" --public-ip 45.133.1.%d", 10+vi) +
						fmt.Sprintf(" --dz-prefixes 45.133.%d.0/30", 10+vi) +
						" --mgmt-vrf default"},
					{name: "device_create_2", cmd: cli + " device create --code " + deviceCode2 +
						" --contributor " + contributorCode +
						" --location " + locationCode +
						" --exchange " + exchangeCode +
						fmt.Sprintf(" --public-ip 45.133.1.%d", 110+vi) +
						fmt.Sprintf(" --dz-prefixes 45.133.%d.128/30", 10+vi) +
						" --mgmt-vrf default"},

					// --- Phase 3: Create device interfaces ---
					// Interfaces are created in "pending" status. The onchain program requires
					// interfaces to be in "unlinked" status before a link can reference them,
					// so we manually transition them after creation.
					// Note: bandwidth and MTU are properties of the link, not the interface.
					{name: "device_interface_create", cmd: cli + " device interface create " + deviceCode + " " + ifaceName},
					{name: "device_interface_create_2", cmd: cli + " device interface create " + deviceCode2 + " " + ifaceName},

					// Transition interfaces from "pending" → "unlinked". This is the status
					// the activator would normally set after verifying the physical interface
					// exists on the device. We do it manually since we don't have real hardware.
					{name: "device_interface_set_unlinked", cmd: cli + " device interface update " + deviceCode + " " + ifaceName +
						" --status unlinked"},
					{name: "device_interface_set_unlinked_2", cmd: cli + " device interface update " + deviceCode2 + " " + ifaceName +
						" --status unlinked"},

					// --- Phase 4: Create WAN link ---
					// The link create instruction references both devices and interfaces. The
					// onchain program validates that both interfaces are in "unlinked" status.
					// Note: --jitter-ms minimum is 0.01 in v0.8.1 (later versions accept 0).
					// Note: bandwidth must be a human-readable string like "10 Gbps".
					{name: "link_create_wan", cmd: cli + " link create wan" +
						" --code " + linkCode +
						" --contributor " + contributorCode +
						" --side-a " + deviceCode + " --side-a-interface " + ifaceName +
						" --side-z " + deviceCode2 + " --side-z-interface " + ifaceName +
						` --bandwidth "10 Gbps" --mtu 9000 --delay-ms 1 --jitter-ms 0.01`},

					// --- Phase 5: Multicast group ---
					// Multicast group create may fail for CLI < 0.8.1 because the
					// MulticastGroupCreateArgs Borsh struct changed in v0.8.1: the index and
					// bump_seed fields were removed. Older CLIs send the old format which
					// causes Borsh deserialization failure in the current program.
					{name: "multicast_group_create", cmd: cli + " multicast group create --code " + multicastCode +
						" --max-bandwidth 100 --owner me", noCascade: true},

					// --- Phase 6: Update operations on newly created entities ---
					// Test that update instructions from the old CLI are compatible.
					// Location and exchange updates accept codes as --pubkey in all versions.
					{name: "location_update", cmd: cli + " location update --pubkey " + locationCode + " --name TestLocUpdated"},
					{name: "exchange_update", cmd: cli + " exchange update --pubkey " + exchangeCode + " --name TestExchangeUpdated"},
					// Contributor update requires a real base58 pubkey for both --pubkey and
					// --owner in older CLIs (v0.8.1 accepts "me" for create but not update).
					{name: "contributor_update", cmd: cli + " contributor update --pubkey " + lookupPubkeyByCode("contributor list", contributorCode) + " --owner " + managerPubkey},
					// Device update requires --pubkey in older CLIs (current CLI can use --code alone).
					{name: "device_update", cmd: cli + " device update --pubkey " + lookupPubkeyByCode("device list", deviceCode) + " --code " + deviceCode +
						fmt.Sprintf(" --public-ip 45.133.2.%d", 10+vi)},

					// --- Phase 7: Update operations on existing cloned entities ---
					// These entities were cloned from the remote cluster. We update them using the
					// old CLI to verify that updates to pre-existing onchain accounts work.
					// We use the current CLI's table output to discover entity pubkeys.
					{name: "cloned_location_update", cmd: cli + " location update --pubkey " + lookupFirstPubkey("location list") + " --name ClonedLocUpdated"},
					{name: "cloned_exchange_update", cmd: cli + " exchange update --pubkey " + lookupFirstPubkey("exchange list") + " --name ClonedExUpdated"},

					// --- Phase 8: Verify state ---
					// Confirm that entities created/updated by the old CLI are visible.
					{name: "device_list_verify", cmd: cli + " device list"},
					{name: "link_list_verify", cmd: cli + " link list"},

					// --- Phase 9: Delete operations ---
					// Test delete instructions in reverse dependency order.
					// The onchain program enforces referential integrity:
					//   - A link must be deleted before its interfaces can be modified
					//   - Interfaces must be deleted before their device can be deleted
					//   - An interface must be in "activated" or "unlinked" status to be deleted

					// Wait for the activator to assign a tunnel_net to the link.
					// After activation the status becomes "provisioning" (not "activated").
					// We check that tunnel_net is no longer the default 0.0.0.0/0.
					{name: "link_wait_activated", cmd: `for i in $(seq 1 300); do doublezero link list 2>/dev/null | grep '` + linkCode + `' | grep -qv '0.0.0.0/0' && exit 0; sleep 1; done; echo "link not activated after 300 attempts"; exit 1`},

					// Link delete requires a real base58 pubkey (not a code) in older CLIs.
					{name: "link_delete", cmd: cli + " link delete --pubkey " + lookupPubkeyByCode("link list", linkCode)},

					// Wait for the activator to process the link deletion and transition
					// the interfaces back to "unlinked" status (CloseAccountLink decrements
					// device.reference_count and restores interfaces to unlinked).
					{name: "iface_wait_unlinked", cmd: `for i in $(seq 1 60); do doublezero device interface list ` + deviceCode + ` 2>/dev/null | grep '` + ifaceName + `' | grep -q unlinked && exit 0; sleep 1; done; echo "interface not unlinked after 60s"; exit 1`},

					{name: "device_interface_delete", cmd: cli + " device interface delete " + deviceCode + " " + ifaceName},
					{name: "device_interface_delete_2", cmd: cli + " device interface delete " + deviceCode2 + " " + ifaceName},

					// Wait for the activator to process the interface deletions and
					// remove them from the device's interfaces array. The CLI delete
					// only marks them as "Deleting"; the activator calls
					// RemoveDeviceInterface to actually remove the entries.
					{name: "iface_wait_removed", cmd: `for i in $(seq 1 60); do count=$(doublezero device interface list ` + deviceCode + ` 2>/dev/null | tail -n +2 | wc -l); [ "$count" -eq 0 ] && exit 0; sleep 1; done; echo "interfaces not removed after 60s"; exit 1`},
					{name: "iface_wait_removed_2", cmd: `for i in $(seq 1 60); do count=$(doublezero device interface list ` + deviceCode2 + ` 2>/dev/null | tail -n +2 | wc -l); [ "$count" -eq 0 ] && exit 0; sleep 1; done; echo "interfaces not removed after 60s"; exit 1`},

					{name: "device_delete", cmd: cli + " device delete --pubkey " + lookupPubkeyByCode("device list", deviceCode)},
					{name: "device_delete_2", cmd: cli + " device delete --pubkey " + lookupPubkeyByCode("device list", deviceCode2)},
				}

				writeFailed := false
				for _, ws := range writeSteps {
					ws := ws
					t.Run(ws.name, func(t *testing.T) {
						if writeFailed {
							recordResult(version, "write/"+ws.name, "SKIP", "previous step failed")
							t.Skip("skipped: previous write step failed")
						}
						log.Info("==> Running manager write command", "command", ws.cmd)
						output, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", ws.cmd})
						stepKey := "write/" + ws.name
						if err == nil {
							recordResult(version, stepKey, "PASS", "")
							log.Info("--> Command succeeded", "command", ws.cmd)
						} else if isKnownIncompatible(stepKey, version) {
							// Known incompatibility - record but don't fail the test
							recordResult(version, stepKey, "KNOWN_FAIL", string(output))
							log.Info("--> Command failed (known incompatibility)", "command", ws.cmd)
						} else {
							// Unexpected failure - fail the test
							assert.NoError(t, err, "command %q failed: %s", ws.cmd, string(output))
							recordResult(version, stepKey, "FAIL", string(output))
							if !ws.noCascade {
								writeFailed = true
							}
						}
					})
				}
			})
		})
	}
}
