//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
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

	// All multicast operations that depend on multicast_group_create. When the group
	// can't be created (< 0.8.1), these all fail with "MulticastGroup not found".
	"write/multicast_group_wait_activated":       "0.8.1",
	"write/multicast_group_update":               "0.8.1",
	"write/multicast_group_pub_allowlist_add":    "0.8.1",
	"write/multicast_group_pub_allowlist_remove": "0.8.1",
	"write/multicast_group_sub_allowlist_add":    "0.8.1",
	"write/user_subscribe":                       "0.8.1",
	"write/multicast_group_sub_allowlist_remove": "0.8.1",
	"write/multicast_group_get":                  "0.8.1",
	"write/multicast_group_delete":               "0.8.1",

	// set-health commands: Added in v0.8.6 as part of Network Provisioning.
	// Older CLIs don't have these subcommands.
	"write/device_set_health":   "0.8.6",
	"write/device_set_health_2": "0.8.6",
	"write/link_set_health":     "0.8.6",
	"write/link_set_health_dzx": "0.8.6",

	// global_config_set: The SetGlobalConfig instruction added new required accounts
	// that released CLIs (through v0.8.6) don't include, causing "insufficient account
	// keys for instruction". Fixed in unreleased code.
	"write/global_config_set": "0.8.7",
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
// Architecture (two-phase):
//
//	Discovery phase: One lightweight devnet clones from the remote cluster, deploys
//	the upgraded program, and enumerates compatible CLI versions.
//
//	Per-version phase: N parallel devnets, each cloning from the discovery devnet's
//	local RPC (fast ~10-30s instead of slow remote clone), running full read + write
//	workflows independently with isolated onchain state.
//
// Environment variables:
//   - DZ_COMPAT_CLONE_ENV: comma-separated environments to test (default: "testnet,mainnet-beta")
//   - DZ_COMPAT_MIN_VERSION: override ProgramConfig.MinCompatVersion (e.g., "0.8.1")
//   - DZ_COMPAT_MAX_NUM_VERSIONS: limit number of versions to test (0 = all, e.g., "2")
//   - DZ_COMPAT_MAX_CONCURRENCY: max parallel version stacks (default: 12)
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

	// Track footnotes for failures.
	var footnotes []struct {
		num     int
		version string
		step    string
		err     string
	}
	footnoteNum := 0

	for _, name := range stepNames {
		buf.WriteString(fmt.Sprintf("%-*s", maxNameLen+2, name))
		for _, version := range r.versions {
			res, ok := lookup[version][name]
			if !ok {
				buf.WriteString(fmt.Sprintf("  %-10s", "-"))
			} else if res.status == "FAIL" && res.err != "" {
				footnoteNum++
				buf.WriteString(fmt.Sprintf("  %-10s", fmt.Sprintf("FAIL [%d]", footnoteNum)))
				footnotes = append(footnotes, struct {
					num     int
					version string
					step    string
					err     string
				}{footnoteNum, version, name, res.err})
			} else {
				buf.WriteString(fmt.Sprintf("  %-10s", res.status))
			}
		}
		buf.WriteString("\n")
	}

	// Print footnotes for failures.
	if len(footnotes) > 0 {
		buf.WriteString("\n")
		for _, fn := range footnotes {
			// Truncate error to first line and max 200 chars for readability.
			errMsg := fn.err
			if idx := strings.IndexByte(errMsg, '\n'); idx != -1 {
				errMsg = errMsg[:idx]
			}
			if len(errMsg) > 200 {
				errMsg = errMsg[:200] + "..."
			}
			buf.WriteString(fmt.Sprintf("  [%d] %s\n", fn.num, errMsg))
		}
	}

	// Print focused failures section.
	var failures []struct {
		version string
		step    string
		err     string
	}
	for _, version := range r.versions {
		for _, res := range r.matrix[version] {
			if res.status == "FAIL" {
				failures = append(failures, struct {
					version string
					step    string
					err     string
				}{version, res.name, res.err})
			}
		}
	}
	if len(failures) > 0 {
		buf.WriteString(fmt.Sprintf("\n=== %s: Failures ===\n\n", r.env))
		for _, f := range failures {
			errMsg := f.err
			if idx := strings.IndexByte(errMsg, '\n'); idx != -1 {
				errMsg = errMsg[:idx]
			}
			if len(errMsg) > 200 {
				errMsg = errMsg[:200] + "..."
			}
			buf.WriteString(fmt.Sprintf("  %-11s  %-40s  %s\n", formatVersionLabel(f.version), f.step, errMsg))
		}
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

// containerAccessibleHost returns the host address that Docker containers can use
// to reach the host machine's exposed ports. In CI (Docker-in-Docker), this is the
// DIND_LOCALHOST env var. On Docker Desktop (Mac/Windows), it's host.docker.internal.
func containerAccessibleHost() string {
	if h := os.Getenv("DIND_LOCALHOST"); h != "" {
		return h
	}
	return "host.docker.internal"
}

// getMaxConcurrency returns the maximum number of parallel version stacks to run.
// Controlled by DZ_COMPAT_MAX_CONCURRENCY env var (default: 12, matching CI -parallel).
func getMaxConcurrency() int {
	if s := os.Getenv("DZ_COMPAT_MAX_CONCURRENCY"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 12
}

// createAndStartDiscoveryDevnet creates a minimal devnet that clones state from a
// remote cluster and deploys the upgraded program. It only starts the components
// needed for version discovery: network, ledger, manager, and funder.
//
// Returns the devnet, the manager pubkey, and the program ID.
func createAndStartDiscoveryDevnet(t *testing.T, cloneEnv string, log *slog.Logger) (*devnet.Devnet, string, string) {
	deployID := "dz-e2e-Compat-" + cloneEnv + "-disc-" + random.ShortID()

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	programID, ok := serviceability.ProgramIDs[cloneEnv]
	require.True(t, ok, "unknown environment %q (valid: mainnet-beta, testnet, devnet)", cloneEnv)
	rpcURL, ok := serviceability.LedgerRPCURLs[cloneEnv]
	require.True(t, ok, "no RPC URL for environment %q", cloneEnv)
	log.Debug("==> Creating discovery devnet", "env", cloneEnv, "programID", programID)

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

	log.Debug("==> Starting discovery devnet with cloned state and upgraded program",
		"programID", programID,
		"upgradeAuthority", managerPubkey,
	)

	// Start only the components needed for discovery. We skip the activator and
	// controller since they aren't needed just to enumerate versions.
	_, err = dn.DefaultNetwork.CreateIfNotExists(t.Context())
	require.NoError(t, err)
	_, err = dn.Ledger.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	_, err = dn.Manager.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	_, err = dn.Funder.StartIfNotRunning(t.Context())
	require.NoError(t, err)

	// Configure the manager CLI to use the cloned program ID.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		fmt.Sprintf("doublezero config set --program-id %s", programID)})
	require.NoError(t, err)

	// Fund the manager account since we skipped program deployment which normally does this.
	_, err = dn.Manager.Exec(t.Context(), []string{"solana", "airdrop", "100"})
	require.NoError(t, err)

	// Initialize the smart contract. This creates ProgramConfig, GlobalState, and other
	// PDA accounts that the upgraded program needs.
	log.Debug("==> Initializing smart contract")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero init"})
	require.NoError(t, err)

	log.Debug("--> Discovery devnet started")
	return dn, managerPubkey, programID
}

// discoverVersions enumerates CLI versions compatible with the current onchain program
// using the discovery devnet's manager container to query Cloudsmith.
func discoverVersions(t *testing.T, dn *devnet.Devnet, cloneEnv, programID string, log *slog.Logger) []string {
	// Read ProgramConfig to get min_compatible_version and current version.
	log.Debug("==> Reading ProgramConfig from ledger")
	svcClient, err := devnet.NewServiceabilityClientForProgram(dn, programID)
	require.NoError(t, err)

	programData, err := svcClient.GetProgramData(t.Context())
	require.NoError(t, err)
	require.NotNil(t, programData.ProgramConfig, "ProgramConfig not found onchain")

	minVersion := programData.ProgramConfig.MinCompatVersion
	currentVersion := programData.ProgramConfig.Version

	// DZ_COMPAT_MIN_VERSION overrides ProgramConfig.MinCompatVersion.
	if override := os.Getenv("DZ_COMPAT_MIN_VERSION"); override != "" {
		pv, ok := devnet.ParseSemver(override)
		require.True(t, ok, "invalid DZ_COMPAT_MIN_VERSION: %s", override)
		minVersion = pv
	}

	log.Debug("--> ProgramConfig",
		"version", devnet.FormatProgramVersion(currentVersion),
		"minCompatVersion", devnet.FormatProgramVersion(minVersion),
	)

	// Set up Cloudsmith repo in the manager container.
	log.Debug("==> Setting up Cloudsmith repo in discovery manager")
	managerExec := func(ctx context.Context, command []string) ([]byte, error) {
		return dn.Manager.Exec(ctx, command)
	}
	err = devnet.SetupCloudsmithRepo(t.Context(), managerExec, cloneEnv)
	require.NoError(t, err)

	// Enumerate available versions from the Cloudsmith apt repo.
	aptOutput, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		"apt-cache madison doublezero | awk -F'|' '{print $2}' | sed 's/ //g; s/-1$//' | sort -uV"})
	require.NoError(t, err)
	aptVersions := strings.Split(strings.TrimSpace(string(aptOutput)), "\n")
	for i, v := range aptVersions {
		if !strings.HasPrefix(v, "v") {
			aptVersions[i] = "v" + v
		}
	}

	compatVersions := devnet.EnumerateCompatibleVersions(aptVersions, minVersion, currentVersion)

	// DZ_COMPAT_MAX_NUM_VERSIONS limits how many versions to test (0 = all).
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
	return compatVersions
}

// createAndStartVersionDevnet creates an isolated devnet for testing a single CLI version.
// It clones from the discovery devnet's local RPC (fast) instead of the remote cluster (slow).
func createAndStartVersionDevnet(
	t *testing.T,
	cloneEnv string,
	version string,
	vi int,
	discoveryRPCURL string,
	programID string,
	svcKeypairPath string,
	envConfig compatEnvConfig,
	log *slog.Logger,
) (*devnet.Devnet, string) {
	versionID := version
	if version == devnet.CurrentVersionLabel {
		versionID = "cur"
	}
	deployID := "dz-e2e-Compat-" + cloneEnv + "-" + versionID + "-" + random.ShortID()

	log.Debug("==> Creating version devnet", "version", version, "deployID", deployID)

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),
		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Ledger: devnet.LedgerSpec{
			// Clone from the discovery devnet's local RPC — much faster than remote.
			CloneFromURL:    discoveryRPCURL,
			CloneProgramIDs: []string{programID},
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: svcKeypairPath,
			ServiceabilityProgramID:          programID,
		},
		Activator: devnet.ActivatorSpec{
			OnchainAllocation: devnet.BoolPtr(envConfig.OnchainAllocation),
		},
		SkipProgramDeploy: true,
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	// Read the manager pubkey for the upgrade authority.
	managerKeypairJSON, err := os.ReadFile(dn.Spec.Manager.ManagerKeypairPath)
	require.NoError(t, err)
	managerPubkey, err := solana.PubkeyFromKeypairJSON(managerKeypairJSON)
	require.NoError(t, err)

	// Configure the ledger to deploy the upgraded program at startup.
	dn.Spec.Ledger.UpgradeProgramID = programID
	dn.Spec.Ledger.UpgradeProgramSOPath = devnet.UpgradeProgramContainerSOPath
	dn.Spec.Ledger.UpgradeAuthority = managerPubkey
	dn.Spec.Ledger.PatchGlobalStateAuthority = managerPubkey

	// Start components individually. We need to init the smart contract before
	// starting the activator (it needs ProgramConfig and GlobalConfig PDAs).
	_, err = dn.DefaultNetwork.CreateIfNotExists(t.Context())
	require.NoError(t, err)
	_, err = dn.Ledger.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	_, err = dn.Manager.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	_, err = dn.Funder.StartIfNotRunning(t.Context())
	require.NoError(t, err)

	// Configure the manager CLI to use the cloned program ID.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		fmt.Sprintf("doublezero config set --program-id %s", programID)})
	require.NoError(t, err)

	// Fund the manager account.
	_, err = dn.Manager.Exec(t.Context(), []string{"solana", "airdrop", "100"})
	require.NoError(t, err)

	// Initialize the smart contract.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero init"})
	require.NoError(t, err)

	// Start the activator — it needs the PDAs to exist.
	// Skip the controller (not exercised in compat tests, saves memory).
	_, err = dn.Activator.StartIfNotRunning(t.Context())
	require.NoError(t, err)

	// Set up Cloudsmith and install the CLI version for this stack.
	managerExec := func(ctx context.Context, command []string) ([]byte, error) {
		return dn.Manager.Exec(ctx, command)
	}
	if version != devnet.CurrentVersionLabel {
		err = devnet.SetupCloudsmithRepo(t.Context(), managerExec, cloneEnv)
		require.NoError(t, err)
		log.Debug("==> Installing CLI version", "version", version)
		err = devnet.InstallCLIVersion(t.Context(), managerExec, version)
		require.NoError(t, err)
	}

	log.Debug("--> Version devnet started", "version", version)
	return dn, managerPubkey
}

func testBackwardCompatibilityForEnv(t *testing.T, cloneEnv string, envResults *compatEnvResults) {
	log := newTestLoggerForTest(t)

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	// --- Phase 1: Discovery ---
	// Create a lightweight devnet that clones from the remote cluster, deploys the
	// upgraded program, and enumerates compatible CLI versions.
	discoveryDn, _, programID := createAndStartDiscoveryDevnet(t, cloneEnv, log)

	// Enumerate compatible versions using the discovery devnet.
	compatVersions := discoverVersions(t, discoveryDn, cloneEnv, programID, log)

	if len(compatVersions) == 0 {
		t.Skip("no compatible versions found between min_compatible_version and current version")
	}

	// Store the version list in the shared results so the parent can render the matrix.
	envResults.versions = compatVersions

	// Build the RPC URL that per-version devnet containers can use to clone from
	// the discovery devnet's ledger. We use the host-exposed port so containers
	// on different Docker networks can reach it.
	discoveryRPCURL := fmt.Sprintf("http://%s:%d",
		containerAccessibleHost(), discoveryDn.Ledger.ExternalRPCPort)
	log.Debug("--> Discovery RPC URL for version cloning", "url", discoveryRPCURL)

	// Keep the discovery devnet alive until all version subtests finish.
	// Go's testing framework ensures the parent doesn't return until all parallel
	// subtests complete, and t.Cleanup runs after that.
	t.Cleanup(func() {
		log.Debug("==> Destroying discovery devnet")
		if err := discoveryDn.Destroy(context.Background(), false); err != nil {
			log.Error("Failed to destroy discovery devnet", "error", err)
		}
	})

	// --- Phase 2: Per-version parallel execution ---
	sem := make(chan struct{}, getMaxConcurrency())

	recordResult := envResults.record

	for vi, version := range compatVersions {
		vi, version := vi, version
		testName := "v" + version
		if version == devnet.CurrentVersionLabel {
			testName = version // no "v" prefix for "current"
		}
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			// Acquire concurrency slot.
			sem <- struct{}{}
			defer func() { <-sem }()

			vLog := log.With("version", version)

			// Create an isolated devnet for this version, cloning from the
			// discovery devnet's local RPC (fast ~10-30s).
			dn, managerPubkey := createAndStartVersionDevnet(
				t, cloneEnv, version, vi,
				discoveryRPCURL, programID, serviceabilityProgramKeypairPath,
				compatEnvConfigs[cloneEnv], vLog,
			)

			// Destroy the version devnet when the subtest finishes.
			t.Cleanup(func() {
				vLog.Debug("==> Destroying version devnet")
				if err := dn.Destroy(context.Background(), false); err != nil {
					vLog.Error("Failed to destroy version devnet", "error", err)
				}
			})

			// Use the unversioned binary for "current" (branch build already in container),
			// versioned binary for released versions (installed from Cloudsmith).
			cli := fmt.Sprintf("doublezero-%s", version)
			if version == devnet.CurrentVersionLabel {
				cli = "doublezero"
			}
			vLog = vLog.With("cli", cli)

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

			// Read and write workflows run in parallel since reads only
			// touch cloned state and don't conflict with writes.
			t.Run("manager_read", func(t *testing.T) {
				t.Parallel()
				runReadWorkflows(t, dn, cli, version, recordResult, vLog)
			})
			t.Run("manager_write", func(t *testing.T) {
				t.Parallel()
				runWriteWorkflows(t, dn, cli, version, vi, cloneEnv, managerPubkey, lookupFirstPubkey, lookupPubkeyByCode, recordResult, vLog)
			})
		})
	}
}

// runReadWorkflows tests that the CLI can deserialize onchain state written by the
// current program.
func runReadWorkflows(
	t *testing.T,
	dn *devnet.Devnet,
	cli, version string,
	recordResult func(version, name, status, errMsg string),
	log *slog.Logger,
) {
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
				recordResult(version, stepKey, "KNOWN_FAIL", string(output))
				log.Debug("--> Command failed (known incompatibility)", "command", rc.cmd)
			} else {
				assert.NoError(t, err, "command %q failed: %s", rc.cmd, string(output))
				recordResult(version, stepKey, "FAIL", string(output))
			}
		})
	}
}

// runWriteWorkflows tests the full entity lifecycle: create, update, verify, delete.
func runWriteWorkflows(
	t *testing.T,
	dn *devnet.Devnet,
	cli, version string,
	vi int,
	cloneEnv string,
	managerPubkey string,
	lookupFirstPubkey func(string) string,
	lookupPubkeyByCode func(string, string) string,
	recordResult func(version, name, status, errMsg string),
	log *slog.Logger,
) {
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
	userClientIP := fmt.Sprintf("45.133.%d.129", 10+vi) // device2: multicast user (banned, can't delete)
	user2ClientIP := fmt.Sprintf("45.133.%d.1", 10+vi)  // device1: non-multicast user (normal delete)

	// dumpDiagnostics logs the current onchain state for debugging failed steps.
	// Uses fmt.Println to preserve newlines in output (structured logging escapes them).
	dumpDiagnostics := func(stepName string) {
		fmt.Printf("=== DIAGNOSTICS [%s %s] failed step: %s ===\n", cloneEnv, formatVersionLabel(version), stepName)
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
		// Add entity-specific get commands based on the failed step name.
		if strings.Contains(stepName, "multicast") || strings.Contains(stepName, "subscribe") {
			diagCmds = append(diagCmds, struct {
				label string
				cmd   string
			}{"multicast group get " + multicastCode, "doublezero multicast group get --code " + multicastCode + " 2>&1"})
		}
		if strings.Contains(stepName, "link") {
			diagCmds = append(diagCmds, struct {
				label string
				cmd   string
			}{"link get " + linkCode, "doublezero link get --code " + linkCode + " 2>&1"},
				struct {
					label string
					cmd   string
				}{"link get " + dzxLinkCode, "doublezero link get --code " + dzxLinkCode + " 2>&1"})
		}
		if strings.Contains(stepName, "device") {
			diagCmds = append(diagCmds, struct {
				label string
				cmd   string
			}{"device get " + deviceCode, "doublezero device get --code " + deviceCode + " 2>&1"},
				struct {
					label string
					cmd   string
				}{"device get " + deviceCode2, "doublezero device get --code " + deviceCode2 + " 2>&1"})
		}
		if strings.Contains(stepName, "user") {
			diagCmds = append(diagCmds, struct {
				label string
				cmd   string
			}{"user get " + userClientIP, fmt.Sprintf("doublezero user get --pubkey $(doublezero user list 2>/dev/null | grep '%s' | awk '{print $1}') 2>&1", userClientIP)},
				struct {
					label string
					cmd   string
				}{"user get " + user2ClientIP, fmt.Sprintf("doublezero user get --pubkey $(doublezero user list 2>/dev/null | grep '%s ' | awk '{print $1}') 2>&1", user2ClientIP)})
		}
		for _, dc := range diagCmds {
			out, _ := dn.Manager.Exec(t.Context(), []string{"bash", "-c", dc.cmd})
			fmt.Printf("  %s:\n%s\n", dc.label, string(out))
		}
		// Dump activator container logs (last 50 lines).
		if dn.Activator.ContainerID != "" {
			logsReader, err := dockerClient.ContainerLogs(t.Context(), dn.Activator.ContainerID, dockercontainer.LogsOptions{
				ShowStdout: true,
				ShowStderr: true,
				Tail:       "50",
			})
			if err != nil {
				fmt.Printf("  activator logs: ERROR: %v\n", err)
			} else {
				var stdout, stderr bytes.Buffer
				_, _ = stdcopy.StdCopy(&stdout, &stderr, logsReader)
				logsReader.Close()
				fmt.Printf("  activator logs (stdout):\n%s\n", stdout.String())
				if stderr.Len() > 0 {
					fmt.Printf("  activator logs (stderr):\n%s\n", stderr.String())
				}
			}
		}
		fmt.Printf("=== END DIAGNOSTICS [%s %s] ===\n", cloneEnv, formatVersionLabel(version))
	}

	type writeStep struct {
		name      string
		cmd       string
		noCascade bool // if true, failure doesn't skip subsequent phases
	}

	// execStep runs a single write step and records the result.
	// Returns true if the step caused a cascade failure.
	execStep := func(t *testing.T, ws writeStep) bool {
		log.Debug("==> Running manager write command", "command", ws.cmd)
		output, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", ws.cmd})
		stepKey := "write/" + ws.name
		if err == nil {
			recordResult(version, stepKey, "PASS", "")
			log.Debug("--> Command succeeded", "command", ws.cmd)
			return false
		}
		if isKnownIncompatible(stepKey, version) {
			recordResult(version, stepKey, "KNOWN_FAIL", string(output))
			log.Debug("--> Command failed (known incompatibility)", "command", ws.cmd)
			return false
		}
		log.Error("Command failed", "step", ws.name, "command", ws.cmd, "output", string(output))
		dumpDiagnostics(ws.name)
		assert.NoError(t, err, "command %q failed: %s", ws.cmd, string(output))
		recordResult(version, stepKey, "FAIL", string(output))
		return !ws.noCascade
	}

	// writePhase groups steps into a phase. Phases run sequentially; if any
	// non-noCascade step fails in a phase, all remaining phases are skipped.
	// When parallel is true, steps within the phase run concurrently.
	// When parallel is false, steps run sequentially (required for entity
	// creates that use counter-based PDA derivation — parallel creates read
	// the same counter and compute conflicting PDAs).
	type writePhase struct {
		name     string
		steps    []writeStep
		parallel bool
	}

	phases := []writePhase{
		// === CREATE PATH ===

		// Foundation creates use counter-based PDA derivation — must be sequential.
		{name: "create_foundation", parallel: false, steps: []writeStep{
			{name: "contributor_create", cmd: cli + " contributor create --code " + contributorCode + " --owner me"},
			{name: "location_create", cmd: cli + " location create --code " + locationCode + " --name TestLoc --country US --lat 40.7 --lng -74.0"},
			{name: "exchange_create", cmd: cli + " exchange create --code " + exchangeCode + " --name TestExchange --lat 40.7 --lng -74.0"},
		}},

		// Device creates use counter-based PDA derivation — must be sequential.
		{name: "create_devices", parallel: false, steps: []writeStep{
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
		}},

		// Device updates are independent and don't use counter-based PDAs.
		{name: "setup_devices", parallel: true, steps: []writeStep{
			{name: "device_set_max_users", cmd: cli + " device update --pubkey " + lookupPubkeyByCode("device list", deviceCode) +
				" --max-users 10"},
			{name: "device_set_max_users_2", cmd: cli + " device update --pubkey " + lookupPubkeyByCode("device list", deviceCode2) +
				" --max-users 10"},
			{name: "device_set_health", cmd: cli + " device set-health --pubkey " + deviceCode +
				" --health ready-for-users", noCascade: true},
			{name: "device_set_health_2", cmd: cli + " device set-health --pubkey " + deviceCode2 +
				" --health ready-for-users", noCascade: true},
			{name: "exchange_set_device", cmd: cli + " exchange set-device --pubkey " + exchangeCode +
				" --device1 " + deviceCode + " --device2 " + deviceCode2, noCascade: true},
		}},

		// Interface creates use counter-based PDA derivation — must be sequential.
		{name: "create_interfaces", parallel: false, steps: []writeStep{
			{name: "device_interface_create", cmd: cli + " device interface create " + deviceCode + " " + ifaceName},
			{name: "device_interface_create_2", cmd: cli + " device interface create " + deviceCode2 + " " + ifaceName},
			{name: "device_interface_create_3", cmd: cli + " device interface create " + deviceCode + " " + ifaceName2},
			{name: "device_interface_create_4", cmd: cli + " device interface create " + deviceCode2 + " " + ifaceName2},
		}},

		// Transition all 4 interfaces to "unlinked" (required before link creation).
		{name: "activate_interfaces", parallel: true, steps: []writeStep{
			{name: "device_interface_set_unlinked", cmd: cli + " device interface update " + deviceCode + " " + ifaceName +
				" --status unlinked"},
			{name: "device_interface_set_unlinked_2", cmd: cli + " device interface update " + deviceCode2 + " " + ifaceName +
				" --status unlinked"},
			{name: "device_interface_set_unlinked_3", cmd: cli + " device interface update " + deviceCode + " " + ifaceName2 +
				" --status unlinked"},
			{name: "device_interface_set_unlinked_4", cmd: cli + " device interface update " + deviceCode2 + " " + ifaceName2 +
				" --status unlinked"},
		}},

		// Link/multicast/accesspass creates use counter-based PDA derivation — must be sequential.
		{name: "create_links_and_entities", parallel: false, steps: []writeStep{
			{name: "link_create_wan", cmd: cli + " link create wan" +
				" --code " + linkCode +
				" --contributor " + contributorCode +
				" --side-a " + deviceCode + " --side-a-interface " + ifaceName +
				" --side-z " + deviceCode2 + " --side-z-interface " + ifaceName +
				` --bandwidth "10 Gbps" --mtu 9000 --delay-ms 1 --jitter-ms 0.01`},
			{name: "link_create_dzx", cmd: cli + " link create dzx" +
				" --code " + dzxLinkCode +
				" --contributor " + contributorCode +
				" --side-a " + deviceCode + " --side-a-interface " + ifaceName2 +
				" --side-z " + deviceCode2 +
				` --bandwidth "10 Gbps" --mtu 9000 --delay-ms 1 --jitter-ms 0.01`},
			{name: "multicast_group_create", cmd: cli + " multicast group create --code " + multicastCode +
				" --max-bandwidth 100 --owner me", noCascade: true},
			{name: "accesspass_set", cmd: cli + " access-pass set --accesspass-type prepaid --client-ip " + userClientIP +
				" --user-payer me", noCascade: true},
			{name: "accesspass_set_2", cmd: cli + " access-pass set --accesspass-type prepaid --client-ip " + user2ClientIP +
				" --user-payer me", noCascade: true},
		}},

		// Accept DZX link + create users. User creates use counter-based PDAs — must be sequential.
		{name: "accept_and_create_users", parallel: false, steps: []writeStep{
			{name: "link_accept_dzx", cmd: cli + " link accept --code " + dzxLinkCode +
				" --side-z-interface " + ifaceName2},
			{name: "user_create", cmd: cli + " user create --device " + deviceCode2 + " --client-ip " + userClientIP, noCascade: true},
			{name: "user_create_2", cmd: cli + " user create --device " + deviceCode + " --client-ip " + user2ClientIP, noCascade: true},
		}},

		// Wait for users to activate + link config — all independent waits.
		{name: "wait_and_configure", parallel: true, steps: []writeStep{
			{name: "link_update", cmd: cli + " link update --pubkey " + lookupPubkeyByCode("link list", dzxLinkCode) +
				` --bandwidth "20 Gbps"`},
			{name: "link_set_health", cmd: cli + " link set-health --pubkey " + linkCode +
				" --health ready-for-service", noCascade: true},
			{name: "link_set_health_dzx", cmd: cli + " link set-health --pubkey " + dzxLinkCode +
				" --health ready-for-service", noCascade: true},
			{name: "user_wait_activated", cmd: `for i in $(seq 1 60); do doublezero user list 2>/dev/null | grep '` + userClientIP + `' | grep -q activated && exit 0; sleep 1; done; echo "user not activated after 60s"; exit 1`, noCascade: true},
			{name: "user_wait_activated_2", cmd: `for i in $(seq 1 60); do doublezero user list 2>/dev/null | grep '` + user2ClientIP + ` ' | grep -q activated && exit 0; sleep 1; done; echo "user2 not activated after 60s"; exit 1`, noCascade: true},
			{name: "multicast_group_wait_activated", cmd: `for i in $(seq 1 60); do doublezero multicast group list 2>/dev/null | grep '` + multicastCode + `' | grep -q activated && exit 0; sleep 1; done; echo "multicast group not activated after 60s"; exit 1`, noCascade: true},
			{name: "multicast_group_update", cmd: cli + " multicast group update --pubkey " + multicastCode +
				" --max-bandwidth 200", noCascade: true},
		}},

		// User update + multicast allowlist operations (need user activated).
		{name: "user_and_multicast_ops", parallel: true, steps: []writeStep{
			{name: "user_update", cmd: cli + " user update --pubkey " +
				fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s' | awk '{print $1}')", userClientIP) +
				" --tunnel-id 999", noCascade: true},
			{name: "multicast_group_pub_allowlist_add", cmd: cli + " multicast group allowlist publisher add --code " + multicastCode +
				" --client-ip " + userClientIP + " --user-payer me", noCascade: true},
		}},

		// Sequential multicast chain: each step depends on the previous.
		{name: "multicast_pub_remove", parallel: false, steps: []writeStep{
			{name: "multicast_group_pub_allowlist_remove", cmd: cli + " multicast group allowlist publisher remove --code " + multicastCode +
				" --client-ip " + userClientIP + " --user-payer me", noCascade: true},
		}},
		{name: "multicast_sub_add", parallel: false, steps: []writeStep{
			{name: "multicast_group_sub_allowlist_add", cmd: cli + " multicast group allowlist subscriber add --code " + multicastCode +
				" --client-ip " + userClientIP + " --user-payer me", noCascade: true},
		}},
		{name: "multicast_subscribe", parallel: false, steps: []writeStep{
			{name: "user_subscribe", cmd: cli + " user subscribe --user " +
				fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s' | awk '{print $1}')", userClientIP) +
				" --group " + multicastCode + " --subscriber", noCascade: true},
		}},
		{name: "multicast_sub_remove", parallel: false, steps: []writeStep{
			{name: "multicast_group_sub_allowlist_remove", cmd: cli + " multicast group allowlist subscriber remove --code " + multicastCode +
				" --client-ip " + userClientIP + " --user-payer me", noCascade: true},
		}},

		// === UPDATE + VERIFY ===

		// All update and get commands are independent reads/writes on existing entities.
		{name: "updates_and_verify", parallel: true, steps: []writeStep{
			{name: "location_update", cmd: cli + " location update --pubkey " + locationCode + " --name TestLocUpdated"},
			{name: "exchange_update", cmd: cli + " exchange update --pubkey " + exchangeCode + " --name TestExchangeUpdated"},
			{name: "contributor_update", cmd: cli + " contributor update --pubkey " + lookupPubkeyByCode("contributor list", contributorCode) + " --owner " + managerPubkey},
			{name: "device_update", cmd: cli + " device update --pubkey " + lookupPubkeyByCode("device list", deviceCode) + " --code " + deviceCode +
				fmt.Sprintf(" --public-ip 45.133.2.%d", 10+vi)},
			{name: "cloned_location_update", cmd: cli + " location update --pubkey " + lookupFirstPubkey("location list") + " --name ClonedLocUpdated"},
			{name: "cloned_exchange_update", cmd: cli + " exchange update --pubkey " + lookupFirstPubkey("exchange list") + " --name ClonedExUpdated"},
			{name: "global_config_set", cmd: cli + " global-config set --remote-asn 65001", noCascade: true},
			{name: "device_list_verify", cmd: cli + " device list"},
			{name: "link_list_verify", cmd: cli + " link list"},
			{name: "contributor_get", cmd: cli + " contributor get --code " + contributorCode, noCascade: true},
			{name: "device_get", cmd: cli + " device get --code " + deviceCode, noCascade: true},
			{name: "exchange_get", cmd: cli + " exchange get --code " + exchangeCode, noCascade: true},
			{name: "link_get", cmd: cli + " link get --code " + linkCode, noCascade: true},
			{name: "location_get", cmd: cli + " location get --code " + locationCode, noCascade: true},
			{name: "multicast_group_get", cmd: cli + " multicast group get --code " + multicastCode, noCascade: true},
			{name: "user_get", cmd: cli + " user get --pubkey " +
				fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s' | awk '{print $1}')", userClientIP), noCascade: true},
			{name: "link_latency", cmd: cli + " link latency", noCascade: true},
		}},

		// === DELETE PATH ===
		// Independent streams run in parallel:
		//   - User1 (multicast): delete → wait removed → close accesspass
		//   - User2 (non-multicast): request-ban → delete → wait removed → close accesspass
		//   - WAN link: wait activated → delete
		//   - DZX link: wait activated → delete

		// Start all 4 delete streams: user1 delete, user2 ban, WAN link wait, DZX link wait.
		{name: "delete_start", parallel: true, steps: []writeStep{
			{name: "user_delete", cmd: cli + " user delete --pubkey " +
				fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s' | awk '{print $1}')", userClientIP)},
			{name: "user_request_ban_2", cmd: cli + " user request-ban --pubkey " +
				fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s ' | awk '{print $1}')", user2ClientIP), noCascade: true},
			{name: "link_wait_activated", cmd: `for i in $(seq 1 60); do doublezero link list 2>/dev/null | grep '` + linkCode + `' | grep -qv '0.0.0.0/0' && exit 0; sleep 1; done; echo "link not activated after 60s"; exit 1`},
			{name: "link_wait_activated_dzx", cmd: `for i in $(seq 1 60); do doublezero link list 2>/dev/null | grep '` + dzxLinkCode + `' | grep -qv '0.0.0.0/0' && exit 0; sleep 1; done; echo "dzx link not activated after 60s"; exit 1`},
		}},

		// Continue all 4 streams: user1 wait, user2 delete, both link deletes.
		{name: "delete_continue", parallel: true, steps: []writeStep{
			{name: "user_wait_removed", cmd: `for i in $(seq 1 30); do ` +
				`count=$(doublezero user list 2>/dev/null | grep '` + userClientIP + `' | wc -l); ` +
				`[ "$count" -eq 0 ] && exit 0; sleep 1; done; ` +
				`echo "user1 not removed after 30s"; exit 1`},
			{name: "user_delete_2", cmd: cli + " user delete --pubkey " +
				fmt.Sprintf("$(doublezero user list 2>/dev/null | grep '%s ' | awk '{print $1}')", user2ClientIP)},
			{name: "link_delete", cmd: cli + " link delete --pubkey " + lookupPubkeyByCode("link list", linkCode)},
			{name: "link_delete_dzx", cmd: cli + " link delete --pubkey " + lookupPubkeyByCode("link list", dzxLinkCode)},
		}},

		// Finish user streams + multicast delete + start interface wait.
		{name: "delete_users_done", parallel: true, steps: []writeStep{
			{name: "accesspass_close", cmd: cli + " access-pass close --pubkey " +
				fmt.Sprintf("$(doublezero access-pass list 2>/dev/null | grep '%s' | awk '{print $1}')", userClientIP)},
			{name: "user_wait_removed_2", cmd: `for i in $(seq 1 30); do ` +
				`count=$(doublezero user list 2>/dev/null | grep '` + user2ClientIP + ` ' | wc -l); ` +
				`[ "$count" -eq 0 ] && exit 0; sleep 1; done; ` +
				`echo "user2 not removed after 30s"; exit 1`},
			{name: "multicast_group_delete", cmd: cli + " multicast group delete --pubkey " + multicastCode, noCascade: true},
			{name: "iface_wait_unlinked", cmd: `for i in $(seq 1 30); do doublezero device interface list ` + deviceCode + ` 2>/dev/null | grep '` + ifaceName + `' | grep -q unlinked && exit 0; sleep 1; done; echo "interface not unlinked after 30s"; exit 1`},
			{name: "iface_wait_unlinked_2", cmd: `for i in $(seq 1 30); do doublezero device interface list ` + deviceCode2 + ` 2>/dev/null | grep '` + ifaceName + `' | grep -q unlinked && exit 0; sleep 1; done; echo "interface not unlinked after 30s"; exit 1`},
			{name: "iface_wait_unlinked_3", cmd: `for i in $(seq 1 30); do doublezero device interface list ` + deviceCode + ` 2>/dev/null | grep '` + ifaceName2 + `' | grep -q unlinked && exit 0; sleep 1; done; echo "interface not unlinked after 30s"; exit 1`},
			{name: "iface_wait_unlinked_4", cmd: `for i in $(seq 1 30); do doublezero device interface list ` + deviceCode2 + ` 2>/dev/null | grep '` + ifaceName2 + `' | grep -q unlinked && exit 0; sleep 1; done; echo "interface not unlinked after 30s"; exit 1`},
		}},

		// Close accesspass2 + delete all interfaces.
		{name: "delete_interfaces", parallel: true, steps: []writeStep{
			{name: "accesspass_close_2", cmd: cli + " access-pass close --pubkey " +
				fmt.Sprintf("$(doublezero access-pass list 2>/dev/null | grep '%s ' | awk '{print $1}')", user2ClientIP)},
			{name: "device_interface_delete", cmd: cli + " device interface delete " + deviceCode + " " + ifaceName},
			{name: "device_interface_delete_2", cmd: cli + " device interface delete " + deviceCode2 + " " + ifaceName},
			{name: "device_interface_delete_3", cmd: cli + " device interface delete " + deviceCode + " " + ifaceName2},
			{name: "device_interface_delete_4", cmd: cli + " device interface delete " + deviceCode2 + " " + ifaceName2},
		}},

		// Wait for interfaces to be removed + clear exchange device refs.
		{name: "wait_interfaces_removed", parallel: true, steps: []writeStep{
			{name: "iface_wait_removed", cmd: `for i in $(seq 1 30); do count=$(doublezero device interface list ` + deviceCode + ` 2>/dev/null | tail -n +2 | wc -l); [ "$count" -eq 0 ] && exit 0; sleep 1; done; echo "interfaces not removed after 30s"; exit 1`},
			{name: "iface_wait_removed_2", cmd: `for i in $(seq 1 30); do count=$(doublezero device interface list ` + deviceCode2 + ` 2>/dev/null | tail -n +2 | wc -l); [ "$count" -eq 0 ] && exit 0; sleep 1; done; echo "interfaces not removed after 30s"; exit 1`},
			{name: "exchange_clear_devices", cmd: cli + " exchange set-device --pubkey " + exchangeCode, noCascade: true},
		}},

		// Delete both devices in parallel.
		{name: "delete_devices", parallel: true, steps: []writeStep{
			{name: "device_delete", cmd: cli + " device delete --pubkey " + lookupPubkeyByCode("device list", deviceCode)},
			{name: "device_delete_2", cmd: cli + " device delete --pubkey " + lookupPubkeyByCode("device list", deviceCode2)},
		}},

		// Wait for both devices to be removed.
		{name: "wait_devices_removed", parallel: true, steps: []writeStep{
			{name: "device_wait_removed", cmd: `for i in $(seq 1 30); do ` +
				`count=$(doublezero device list 2>/dev/null | grep '` + deviceCode + ` ' | wc -l); ` +
				`[ "$count" -eq 0 ] && exit 0; sleep 1; done; ` +
				`echo "device not removed after 30s"; exit 1`},
			{name: "device_wait_removed_2", cmd: `for i in $(seq 1 30); do ` +
				`count=$(doublezero device list 2>/dev/null | grep '` + deviceCode2 + ` ' | wc -l); ` +
				`[ "$count" -eq 0 ] && exit 0; sleep 1; done; ` +
				`echo "device2 not removed after 30s"; exit 1`},
		}},

		// Delete infrastructure entities — all independent.
		{name: "delete_infrastructure", parallel: true, steps: []writeStep{
			{name: "exchange_delete", cmd: cli + " exchange delete --pubkey " + exchangeCode},
			{name: "contributor_delete", cmd: cli + " contributor delete --pubkey " + lookupPubkeyByCode("contributor list", contributorCode)},
			{name: "location_delete", cmd: cli + " location delete --pubkey " + locationCode},
		}},
	}

	// Run phases sequentially. Steps within a phase run concurrently when
	// phase.parallel is true, or sequentially when false (needed for entity
	// creates that use counter-based PDA derivation).
	var phaseFailed atomic.Bool
	for _, phase := range phases {
		phase := phase
		t.Run(phase.name, func(t *testing.T) {
			if phaseFailed.Load() {
				for _, ws := range phase.steps {
					recordResult(version, "write/"+ws.name, "SKIP", "previous phase failed")
				}
				t.Skip("skipped: previous phase failed")
				return
			}

			if phase.parallel {
				var cascadeInPhase atomic.Bool
				for _, ws := range phase.steps {
					ws := ws
					t.Run(ws.name, func(t *testing.T) {
						t.Parallel()
						if execStep(t, ws) {
							cascadeInPhase.Store(true)
						}
					})
				}
				t.Cleanup(func() {
					if cascadeInPhase.Load() {
						phaseFailed.Store(true)
					}
				})
			} else {
				for _, ws := range phase.steps {
					ws := ws
					t.Run(ws.name, func(t *testing.T) {
						if execStep(t, ws) {
							phaseFailed.Store(true)
						}
					})
				}
			}
		})
	}
}
