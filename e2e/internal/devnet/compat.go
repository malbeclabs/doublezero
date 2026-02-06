package devnet

import (
	"context"
	"fmt"
	"sort"
	"strings"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

// CloudsmithRepoName returns the Cloudsmith repo name for the given environment.
// mainnet-beta uses "doublezero", testnet uses "doublezero-testnet", devnet uses "doublezero-devnet".
func CloudsmithRepoName(env string) string {
	switch env {
	case "mainnet-beta":
		return "doublezero"
	case "testnet":
		return "doublezero-testnet"
	case "devnet":
		return "doublezero-devnet"
	default:
		return "doublezero"
	}
}

// SetupCloudsmithRepo runs the one-time Cloudsmith apt repo setup in a container.
func SetupCloudsmithRepo(ctx context.Context, execFn func(ctx context.Context, command []string) ([]byte, error), env string) error {
	repo := CloudsmithRepoName(env)
	_, err := execFn(ctx, []string{"bash", "-c", fmt.Sprintf(`
		set -euo pipefail
		curl -1sLf 'https://dl.cloudsmith.io/public/malbeclabs/%s/setup.deb.sh' | bash
		apt-get update
	`, repo)})
	if err != nil {
		return fmt.Errorf("failed to setup cloudsmith repo: %w", err)
	}
	return nil
}

// InstallCLIVersion installs a specific CLI version from the Cloudsmith apt repo and
// renames the binaries to versioned names (e.g., doublezero-0.7.1, doublezerod-0.7.1).
func InstallCLIVersion(ctx context.Context, execFn func(ctx context.Context, command []string) ([]byte, error), version string) error {
	_, err := execFn(ctx, []string{"bash", "-c", fmt.Sprintf(`
		set -euo pipefail

		# Install the specific version.
		apt-get install -y --allow-downgrades doublezero=%s-1

		# Copy to versioned names (/usr/bin/ is where apt installs them).
		cp /usr/bin/doublezero /usr/local/bin/doublezero-%s
		cp /usr/bin/doublezerod /usr/local/bin/doublezerod-%s 2>/dev/null || true

		# Reinstall the current version (from the container image).
		apt-get install -y --allow-downgrades doublezero 2>/dev/null || true
	`, version, version, version)})
	if err != nil {
		return fmt.Errorf("failed to install CLI version %s: %w", version, err)
	}
	return nil
}

// EnumerateCompatibleVersions returns a sorted list of semver version strings for all
// versions between min (inclusive) and current (exclusive).
// Each entry must be in the format "v<major>.<minor>.<patch>".
func EnumerateCompatibleVersions(versionTags []string, min, current serviceability.ProgramVersion) []string {
	var versions []string
	for _, tag := range versionTags {
		tag = strings.TrimSpace(tag)
		if !strings.HasPrefix(tag, "v") {
			continue
		}
		ver := tag[1:] // strip "v" prefix
		pv, ok := ParseSemver(ver)
		if !ok {
			continue
		}
		if CompareProgramVersions(pv, min) >= 0 && CompareProgramVersions(pv, current) < 0 {
			versions = append(versions, ver)
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		vi, _ := ParseSemver(versions[i])
		vj, _ := ParseSemver(versions[j])
		return CompareProgramVersions(vi, vj) < 0
	})
	return versions
}

func ParseSemver(s string) (serviceability.ProgramVersion, bool) {
	var major, minor, patch uint32
	n, err := fmt.Sscanf(s, "%d.%d.%d", &major, &minor, &patch)
	if err != nil || n != 3 {
		return serviceability.ProgramVersion{}, false
	}
	return serviceability.ProgramVersion{Major: major, Minor: minor, Patch: patch}, true
}

func CompareProgramVersions(a, b serviceability.ProgramVersion) int {
	if a.Major != b.Major {
		return int(a.Major) - int(b.Major)
	}
	if a.Minor != b.Minor {
		return int(a.Minor) - int(b.Minor)
	}
	return int(a.Patch) - int(b.Patch)
}

// FormatProgramVersion formats a ProgramVersion as a semver string.
func FormatProgramVersion(v serviceability.ProgramVersion) string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}
