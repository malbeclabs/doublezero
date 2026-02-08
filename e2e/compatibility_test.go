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
// Example: "write/foo_create": "0.9.0" means:
//   - CLI v0.8.x and older → KNOWN_FAIL (expected to fail, test passes)
//   - CLI v0.9.0 and newer → FAIL if broken (unexpected, test fails)
//
// When adding entries:
//   - Document WHY the incompatibility exists
//   - Set the version to the first CLI version that IS compatible
//   - Remove entries when min_compatible_version is bumped past them
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
	"devnet":       {OnchainAllocation: true},
	"testnet":      {OnchainAllocation: true},
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

// TestE2E_BackwardCompatibility tests CLI compatibility with the upgraded onchain program.
//
// For each CLI version from min_compatible_version through current:
//   - Old versions: Tests backward compatibility - old Borsh instruction formats still work
//   - Current version: Tests that CLI and program from this branch work together
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
//
// compatStepResult tracks the result of a single step for a single version.
type compatStepResult struct {
	name   string
	status string // "PASS", "FAIL", "SKIP", "KNOWN_FAIL"
	err    string // error message if failed
}

// compatEnvResults holds the compatibility matrix results for a single environment.
type compatEnvResults struct {
	env      string
	versions []string                      // ordered list of versions tested
	matrix   map[string][]compatStepResult // version -> results
	mu       sync.Mutex
}

func (r *compatEnvResults) record(version, name, status, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.matrix[version] = append(r.matrix[version], compatStepResult{name, status, errMsg})
}

// formatVersionLabel returns the display label for a version (e.g., "v0.8.1" or "current").
func formatVersionLabel(version string) string {
	if version == devnet.CurrentVersionLabel {
		return version
	}
	return "v" + version
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
		label := formatVersionLabel(version)
		if failed == 0 {
			buf.WriteString(fmt.Sprintf("%-11s  ALL PASSED (%d passed", label, passed))
			if knownFail > 0 {
				buf.WriteString(fmt.Sprintf(", %d known incompatible", knownFail))
			}
			if skipped > 0 {
				buf.WriteString(fmt.Sprintf(", %d skipped", skipped))
			}
			buf.WriteString(")\n")
		} else {
			buf.WriteString(fmt.Sprintf("%-11s  %d passed, %d FAILED", label, passed, failed))
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
		buf.WriteString(fmt.Sprintf("  %-9s", formatVersionLabel(version)))
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
	log := newTestLoggerForTest(t)

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	programID, ok := serviceability.ProgramIDs[cloneEnv]
	require.True(t, ok, "unknown environment %q (valid: mainnet-beta, testnet, devnet)", cloneEnv)
	rpcURL, ok := serviceability.LedgerRPCURLs[cloneEnv]
	require.True(t, ok, "no RPC URL for environment %q", cloneEnv)
	log.Debug("==> Cloning state from environment", "env", cloneEnv, "programID", programID)

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

	log.Debug("==> Starting devnet with cloned state and upgraded program",
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
	log.Debug("==> Initializing smart contract")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero init"})
	require.NoError(t, err)

	// Now start the activator and controller — they need the PDAs to exist.
	_, err = dn.Controller.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	_, err = dn.Activator.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	log.Debug("--> Devnet started")

	// Read ProgramConfig to get min_compatible_version and current version.
	log.Debug("==> Reading ProgramConfig from ledger")

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

	log.Debug("--> ProgramConfig",
		"version", devnet.FormatProgramVersion(currentVersion),
		"minCompatVersion", devnet.FormatProgramVersion(minVersion),
	)

	// Set up Cloudsmith repo in the manager container (needed for version enumeration and installs).
	log.Debug("==> Setting up Cloudsmith repo in manager container")
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

	log.Debug("--> Compatible versions to test", "versions", compatVersions)

	if len(compatVersions) == 0 {
		t.Skip("no compatible versions found between min_compatible_version and current version")
	}

	// Store the version list in the shared results so the parent can render the matrix.
	envResults.versions = compatVersions

	// Install released versions from Cloudsmith. Skip "current" since it's the branch
	// build already in the container as the default `doublezero` binary.
	for _, version := range compatVersions {
		if version == devnet.CurrentVersionLabel {
			log.Debug("==> Skipping install for current (already in container as doublezero)")
			continue
		}
		log.Debug("==> Installing CLI version in manager", "version", version)
		err = devnet.InstallCLIVersion(t.Context(), managerExec, version)
		require.NoError(t, err)
	}

	recordResult := envResults.record

	// Test each compatible version.
	for vi, version := range compatVersions {
		vi, version := vi, version
		testName := "v" + version
		if version == devnet.CurrentVersionLabel {
			testName = version // no "v" prefix for "current"
		}
		t.Run(testName, func(t *testing.T) {
			// Use the unversioned binary for "current" (branch build already in container),
			// versioned binary for released versions (installed from Cloudsmith).
			cli := fmt.Sprintf("doublezero-%s", version)
			if version == devnet.CurrentVersionLabel {
				cli = "doublezero"
			}
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
					{name: "contributor_list", cmd: cli + " contributor list"},
					{name: "accesspass_list", cmd: cli + " access-pass list"},
				}

				for _, rc := range readCommands {
					rc := rc
					t.Run(rc.name, func(t *testing.T) {
						t.Parallel()
						stepKey := "read/" + rc.name
						log.Debug("==> Running manager read command", "command", rc.cmd)
						output, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", rc.cmd})
						if err == nil {
							recordResult(version, stepKey, "PASS", "")
							log.Debug("--> Command succeeded", "command", rc.cmd)
						} else if isKnownIncompatible(stepKey, version) {
							// Known incompatibility - record but don't fail the test
							recordResult(version, stepKey, "KNOWN_FAIL", string(output))
							log.Debug("--> Command failed (known incompatibility)", "command", rc.cmd)
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
				ifaceName2 := "Ethernet2" // For DZX link
				linkCode := "lk" + suffix
				dzxLinkCode := "dx" + suffix
				multicastCode := "mc" + suffix
				// Client IPs must be within each device's dz_prefixes.
				// device1: 45.133.<10+vi>.0/30 → valid host IPs are .1 and .2
				// device2: 45.133.<10+vi>.128/30 → valid host IPs are .129 and .130
				// user1 is multicast user on device2 (will be banned, can't delete → device2 stuck)
				// user2 is non-multicast user on device1 (normal delete → device1 can be cleaned up)
				userClientIP := fmt.Sprintf("45.133.%d.129", 10+vi)  // device2: multicast user (banned, can't delete)
				user2ClientIP := fmt.Sprintf("45.133.%d.1", 10+vi)   // device1: non-multicast user (normal delete)

				// dumpDiagnostics logs the current onchain state for debugging failed steps.
				dumpDiagnostics := func(stepName string) {
					log.Info("=== DIAGNOSTICS for failed step: " + stepName + " ===")
					diagCmds := []struct {
						label string
						cmd   string
					}{
						{"link list", "doublezero link list 2>&1 | head -20"},
						{"device list", "doublezero device list 2>&1 | head -20"},
						{"user list", "doublezero user list 2>&1 | head -20"},
						{"device " + deviceCode + " interfaces", "doublezero device interface list " + deviceCode + " 2>&1"},
						{"device " + deviceCode2 + " interfaces", "doublezero device interface list " + deviceCode2 + " 2>&1"},
						{"access-pass list", "doublezero access-pass list 2>&1 | head -20"},
						{"multicast group list", "doublezero multicast group list 2>&1 | head -20"},
					}
					for _, dc := range diagCmds {
						out, _ := dn.Manager.Exec(t.Context(), []string{"bash", "-c", dc.cmd})
						log.Info("  "+dc.label+":", "output", string(out))
					}
					log.Info("=== END DIAGNOSTICS ===")
				}

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

					// Set max_users on devices so users can be created on them.
					{name: "device_set_max_users", cmd: cli + " device update --pubkey " + lookupPubkeyByCode("device list", deviceCode) +
						" --max-users 10"},
					{name: "device_set_max_users_2", cmd: cli + " device update --pubkey " + lookupPubkeyByCode("device list", deviceCode2) +
						" --max-users 10"},

					// Set device health status (tests device set-health command).
					{name: "device_set_health", cmd: cli + " device set-health --pubkey " + deviceCode +
						" --health ready-for-users", noCascade: true},
					{name: "device_set_health_2", cmd: cli + " device set-health --pubkey " + deviceCode2 +
						" --health ready-for-users", noCascade: true},

					// Set exchange devices (assigns primary devices to the exchange).
					{name: "exchange_set_device", cmd: cli + " exchange set-device --pubkey " + exchangeCode +
						" --device1 " + deviceCode + " --device2 " + deviceCode2, noCascade: true},

					// --- Phase 3: Create device interfaces for WAN link ---
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

					// --- Phase 6: Create device interfaces for DZX link ---
					// Create a second set of interfaces on both devices for the DZX link.
					{name: "device_interface_create_3", cmd: cli + " device interface create " + deviceCode + " " + ifaceName2},
					{name: "device_interface_create_4", cmd: cli + " device interface create " + deviceCode2 + " " + ifaceName2},
					{name: "device_interface_set_unlinked_3", cmd: cli + " device interface update " + deviceCode + " " + ifaceName2 +
						" --status unlinked"},
					{name: "device_interface_set_unlinked_4", cmd: cli + " device interface update " + deviceCode2 + " " + ifaceName2 +
						" --status unlinked"},

					// --- Phase 7: Create DZX link ---
					// DZX links connect devices within the DZ network (as opposed to WAN links
					// which connect to external networks). DZX links are created in "requested"
					// status and must be accepted by the side-z contributor before activation.
					{name: "link_create_dzx", cmd: cli + " link create dzx" +
						" --code " + dzxLinkCode +
						" --contributor " + contributorCode +
						" --side-a " + deviceCode + " --side-a-interface " + ifaceName2 +
						" --side-z " + deviceCode2 +
						` --bandwidth "10 Gbps" --mtu 9000 --delay-ms 1 --jitter-ms 0.01`},
					// Accept the DZX link on the side-z device. This transitions the link from
					// "requested" to "pending", allowing the activator to process it.
					{name: "link_accept_dzx", cmd: cli + " link accept --code " + dzxLinkCode +
						" --side-z-interface " + ifaceName2},

					// --- Phase 8: Link update and health ---
					// Test that link update instructions work.
					{name: "link_update", cmd: cli + " link update --pubkey " + lookupPubkeyByCode("link list", dzxLinkCode) +
						` --bandwidth "20 Gbps"`},
					// Set link health status (tests link set-health command).
					{name: "link_set_health", cmd: cli + " link set-health --pubkey " + linkCode +
						" --health ready-for-service", noCascade: true},
					{name: "link_set_health_dzx", cmd: cli + " link set-health --pubkey " + dzxLinkCode +
						" --health ready-for-service", noCascade: true},

					// --- Phase 9: User lifecycle ---
					// Test the full user lifecycle: access-pass set → user create → wait for
					// activation → user update → user delete → access-pass close.
					// AccessPass must exist before user creation.
					{name: "accesspass_set", cmd: cli + " access-pass set --accesspass-type prepaid --client-ip " + userClientIP +
						" --user-payer me", noCascade: true},
					{name: "user_create", cmd: cli + " user create --device " + deviceCode2 + " --client-ip " + userClientIP, noCascade: true},
					// Wait for user to be activated (status changes from pending).
					{name: "user_wait_activated", cmd: `for i in $(seq 1 60); do doublezero user list 2>/dev/null | grep '` + userClientIP + `' | grep -q activated && exit 0; sleep 1; done; echo "user not activated after 60s"; exit 1`, noCascade: true},
					// Update the user's tunnel ID.
					{name: "user_update", cmd: cli + " user update --pubkey " +
						fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s' | awk '{print $1}')", userClientIP) +
						" --tunnel-id 999", noCascade: true},

					// Create a second user (non-multicast) for testing request-ban.
					// This user won't be subscribed to multicast, so it can be deleted after ban.
					{name: "accesspass_set_2", cmd: cli + " access-pass set --accesspass-type prepaid --client-ip " + user2ClientIP +
						" --user-payer me", noCascade: true},
					{name: "user_create_2", cmd: cli + " user create --device " + deviceCode + " --client-ip " + user2ClientIP, noCascade: true},
					{name: "user_wait_activated_2", cmd: `for i in $(seq 1 60); do doublezero user list 2>/dev/null | grep '` + user2ClientIP + ` ' | grep -q activated && exit 0; sleep 1; done; echo "user2 not activated after 60s"; exit 1`, noCascade: true},

					// --- Phase 10: Multicast group operations ---
					// Test multicast group update and allowlist operations.
					{name: "multicast_group_update", cmd: cli + " multicast group update --pubkey " + multicastCode +
						" --max-bandwidth 200", noCascade: true},
					// Allowlist operations may not exist in older CLIs.
					{name: "multicast_group_pub_allowlist_add", cmd: cli + " multicast group allowlist publisher add --code " + multicastCode +
						" --client-ip " + userClientIP + " --user-payer me", noCascade: true},
					{name: "multicast_group_pub_allowlist_remove", cmd: cli + " multicast group allowlist publisher remove --code " + multicastCode +
						" --client-ip " + userClientIP + " --user-payer me", noCascade: true},
					{name: "multicast_group_sub_allowlist_add", cmd: cli + " multicast group allowlist subscriber add --code " + multicastCode +
						" --client-ip " + userClientIP + " --user-payer me", noCascade: true},
					// Subscribe user to multicast group (must be on allowlist first).
					{name: "user_subscribe", cmd: cli + " user subscribe --user " +
						fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s' | awk '{print $1}')", userClientIP) +
						" --group " + multicastCode + " --subscriber", noCascade: true},
					{name: "multicast_group_sub_allowlist_remove", cmd: cli + " multicast group allowlist subscriber remove --code " + multicastCode +
						" --client-ip " + userClientIP + " --user-payer me", noCascade: true},

					// --- Phase 11: Update operations on newly created entities ---
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

					// --- Phase 12: Update operations on existing cloned entities ---
					// These entities were cloned from the remote cluster. We update them using the
					// old CLI to verify that updates to pre-existing onchain accounts work.
					// We use the current CLI's table output to discover entity pubkeys.
					{name: "cloned_location_update", cmd: cli + " location update --pubkey " + lookupFirstPubkey("location list") + " --name ClonedLocUpdated"},
					{name: "cloned_exchange_update", cmd: cli + " exchange update --pubkey " + lookupFirstPubkey("exchange list") + " --name ClonedExUpdated"},

					// --- Phase 13: Global config operations ---
					// Test global config modification.
					{name: "global_config_set", cmd: cli + " global-config set --remote-asn 65001", noCascade: true},
					// Note: global-config set-version requires foundation allowlist membership
					// and additional account keys that aren't easily set up in this test.

					// --- Phase 14: Verify state and get commands ---
					// Confirm that entities created/updated by the old CLI are visible.
					{name: "device_list_verify", cmd: cli + " device list"},
					{name: "link_list_verify", cmd: cli + " link list"},
					// Test get commands using the entities we just created.
					{name: "contributor_get", cmd: cli + " contributor get --code " + contributorCode, noCascade: true},
					{name: "device_get", cmd: cli + " device get --code " + deviceCode, noCascade: true},
					{name: "exchange_get", cmd: cli + " exchange get --code " + exchangeCode, noCascade: true},
					{name: "link_get", cmd: cli + " link get --code " + linkCode, noCascade: true},
					{name: "location_get", cmd: cli + " location get --code " + locationCode, noCascade: true},
					{name: "multicast_group_get", cmd: cli + " multicast group get --code " + multicastCode, noCascade: true},
					{name: "user_get", cmd: cli + " user get --pubkey " +
						fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s' | awk '{print $1}')", userClientIP), noCascade: true},
					// Test link latency command.
					{name: "link_latency", cmd: cli + " link latency", noCascade: true},

					// --- Phase 15: Delete operations ---
					// Test delete instructions in reverse dependency order.
					// The onchain program enforces referential integrity:
					//   - Users must be deleted before accesspasses can be closed
					//   - A link must be deleted before its interfaces can be modified
					//   - Interfaces must be deleted before their device can be deleted
					//   - An interface must be in "activated" or "unlinked" status to be deleted
					//   - Devices must be deleted before contributor/location/exchange

					// Delete user1 (multicast) first, before banning user2.
					// User1 is subscribed to multicast - delete unsubscribes automatically.
					// Note: If we banned user1 first, delete would fail because SDK's unsubscribe
					// requires Activated status. See plans/sdk-user-delete-banned-multicast-issue.md.
					{name: "user_delete", cmd: cli + " user delete --pubkey " +
						fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s' | awk '{print $1}')", userClientIP)},
					// Wait for user1 to be fully removed from device2.
					{name: "user_wait_removed", cmd: `for i in $(seq 1 30); do ` +
						`count=$(doublezero user list 2>/dev/null | grep '` + userClientIP + `' | wc -l); ` +
						`[ "$count" -eq 0 ] && exit 0; sleep 1; done; ` +
						`echo "user1 not removed after 30s"; exit 1`},
					{name: "accesspass_close", cmd: cli + " access-pass close --pubkey " +
						fmt.Sprintf("$(doublezero access-pass list 2>/dev/null | grep '%s' | awk '{print $1}')", userClientIP)},

					// Test user2 (non-multicast) request-ban and delete workflow.
					// User2 is not subscribed to any multicast groups, so delete works after ban.
					{name: "user_request_ban_2", cmd: cli + " user request-ban --pubkey " +
						fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s ' | awk '{print $1}')", user2ClientIP), noCascade: true},
					{name: "user_delete_2", cmd: cli + " user delete --pubkey " +
						fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s ' | awk '{print $1}')", user2ClientIP)},
					// Wait for user2 to be fully removed from device1's user count.
					{name: "user_wait_removed_2", cmd: `for i in $(seq 1 30); do ` +
						`count=$(doublezero user list 2>/dev/null | grep '` + user2ClientIP + ` ' | wc -l); ` +
						`[ "$count" -eq 0 ] && exit 0; sleep 1; done; ` +
						`echo "user2 not removed after 30s"; exit 1`},
					{name: "accesspass_close_2", cmd: cli + " access-pass close --pubkey " +
						fmt.Sprintf("$(doublezero access-pass list 2>/dev/null | grep '%s ' | awk '{print $1}')", user2ClientIP)},

					// Delete multicast group after users are removed.
					{name: "multicast_group_delete", cmd: cli + " multicast group delete --pubkey " + multicastCode, noCascade: true},

					// Wait for the activator to assign a tunnel_net to the WAN link.
					// After activation the status becomes "provisioning" (not "activated").
					// We check that tunnel_net is no longer the default 0.0.0.0/0.
					{name: "link_wait_activated", cmd: `for i in $(seq 1 60); do doublezero link list 2>/dev/null | grep '` + linkCode + `' | grep -qv '0.0.0.0/0' && exit 0; sleep 1; done; echo "link not activated after 60s"; exit 1`},

					// Link delete requires a real base58 pubkey (not a code) in older CLIs.
					{name: "link_delete", cmd: cli + " link delete --pubkey " + lookupPubkeyByCode("link list", linkCode)},

					// Wait for the DZX link to be activated (tunnel_net assigned).
					{name: "link_wait_activated_dzx", cmd: `for i in $(seq 1 60); do doublezero link list 2>/dev/null | grep '` + dzxLinkCode + `' | grep -qv '0.0.0.0/0' && exit 0; sleep 1; done; echo "dzx link not activated after 60s"; exit 1`},
					{name: "link_delete_dzx", cmd: cli + " link delete --pubkey " + lookupPubkeyByCode("link list", dzxLinkCode)},

					// Wait for the activator to process the link deletion and transition
					// the interfaces back to "unlinked" status (CloseAccountLink decrements
					// device.reference_count and restores interfaces to unlinked).
					{name: "iface_wait_unlinked", cmd: `for i in $(seq 1 30); do doublezero device interface list ` + deviceCode + ` 2>/dev/null | grep '` + ifaceName + `' | grep -q unlinked && exit 0; sleep 1; done; echo "interface not unlinked after 30s"; exit 1`},
					{name: "iface_wait_unlinked_2", cmd: `for i in $(seq 1 30); do doublezero device interface list ` + deviceCode2 + ` 2>/dev/null | grep '` + ifaceName + `' | grep -q unlinked && exit 0; sleep 1; done; echo "interface not unlinked after 30s"; exit 1`},
					{name: "iface_wait_unlinked_3", cmd: `for i in $(seq 1 30); do doublezero device interface list ` + deviceCode + ` 2>/dev/null | grep '` + ifaceName2 + `' | grep -q unlinked && exit 0; sleep 1; done; echo "interface not unlinked after 30s"; exit 1`},
					{name: "iface_wait_unlinked_4", cmd: `for i in $(seq 1 30); do doublezero device interface list ` + deviceCode2 + ` 2>/dev/null | grep '` + ifaceName2 + `' | grep -q unlinked && exit 0; sleep 1; done; echo "interface not unlinked after 30s"; exit 1`},

					// Delete all interfaces (Ethernet1 and Ethernet2 on both devices).
					{name: "device_interface_delete", cmd: cli + " device interface delete " + deviceCode + " " + ifaceName},
					{name: "device_interface_delete_2", cmd: cli + " device interface delete " + deviceCode2 + " " + ifaceName},
					{name: "device_interface_delete_3", cmd: cli + " device interface delete " + deviceCode + " " + ifaceName2},
					{name: "device_interface_delete_4", cmd: cli + " device interface delete " + deviceCode2 + " " + ifaceName2},

					// Wait for the activator to process the interface deletions and
					// remove them from the device's interfaces array. The CLI delete
					// only marks them as "Deleting"; the activator calls
					// RemoveDeviceInterface to actually remove the entries.
					{name: "iface_wait_removed", cmd: `for i in $(seq 1 30); do count=$(doublezero device interface list ` + deviceCode + ` 2>/dev/null | tail -n +2 | wc -l); [ "$count" -eq 0 ] && exit 0; sleep 1; done; echo "interfaces not removed after 30s"; exit 1`},
					{name: "iface_wait_removed_2", cmd: `for i in $(seq 1 30); do count=$(doublezero device interface list ` + deviceCode2 + ` 2>/dev/null | tail -n +2 | wc -l); [ "$count" -eq 0 ] && exit 0; sleep 1; done; echo "interfaces not removed after 30s"; exit 1`},

					// Clear exchange set-device references before deleting devices.
					// This decrements reference_count on both devices by removing the
					// exchange's device1_pk and device2_pk pointers.
					{name: "exchange_clear_devices", cmd: cli + " exchange set-device --pubkey " + exchangeCode, noCascade: true},

					// Delete both devices. Both users were deleted, so reference_count is 0.
					{name: "device_delete", cmd: cli + " device delete --pubkey " + lookupPubkeyByCode("device list", deviceCode)},
					{name: "device_delete_2", cmd: cli + " device delete --pubkey " + lookupPubkeyByCode("device list", deviceCode2)},

					// Wait for devices to be fully removed before deleting exchange.
					// Device deletion may take time to process and release the exchange reference.
					{name: "device_wait_removed", cmd: `for i in $(seq 1 30); do ` +
						`count=$(doublezero device list 2>/dev/null | grep '` + deviceCode + ` ' | wc -l); ` +
						`[ "$count" -eq 0 ] && exit 0; sleep 1; done; ` +
						`echo "device not removed after 30s"; exit 1`},
					{name: "device_wait_removed_2", cmd: `for i in $(seq 1 30); do ` +
						`count=$(doublezero device list 2>/dev/null | grep '` + deviceCode2 + ` ' | wc -l); ` +
						`[ "$count" -eq 0 ] && exit 0; sleep 1; done; ` +
						`echo "device2 not removed after 30s"; exit 1`},

					// Delete exchange, contributor, and location.
					{name: "exchange_delete", cmd: cli + " exchange delete --pubkey " + exchangeCode},
					{name: "contributor_delete", cmd: cli + " contributor delete --pubkey " + lookupPubkeyByCode("contributor list", contributorCode)},
					{name: "location_delete", cmd: cli + " location delete --pubkey " + locationCode},
				}

				writeFailed := false
				for _, ws := range writeSteps {
					ws := ws
					t.Run(ws.name, func(t *testing.T) {
						if writeFailed {
							recordResult(version, "write/"+ws.name, "SKIP", "previous step failed")
							t.Skip("skipped: previous write step failed")
						}
						log.Debug("==> Running manager write command", "command", ws.cmd)
						output, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", ws.cmd})
						stepKey := "write/" + ws.name
						if err == nil {
							recordResult(version, stepKey, "PASS", "")
							log.Debug("--> Command succeeded", "command", ws.cmd)
						} else if isKnownIncompatible(stepKey, version) {
							// Known incompatibility - record but don't fail the test
							recordResult(version, stepKey, "KNOWN_FAIL", string(output))
							log.Debug("--> Command failed (known incompatibility)", "command", ws.cmd)
						} else {
							// Unexpected failure - dump diagnostics and fail the test
							log.Error("Command failed", "step", ws.name, "command", ws.cmd, "output", string(output))
							dumpDiagnostics(ws.name)
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
