package devnet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

// FetchGitHubClientReleaseTags fetches all client release tags from the GitHub API.
// Returns version strings in "vX.Y.Z" format (e.g. "v0.10.0").
// githubToken is used for authentication; pass empty string for unauthenticated access.
func FetchGitHubClientReleaseTags(ctx context.Context, githubToken string) ([]string, error) {
	var allTags []string
	for page := 1; ; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/malbeclabs/doublezero/releases?per_page=100&page=%d", page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if githubToken != "" {
			req.Header.Set("Authorization", "Bearer "+githubToken)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch releases page %d: %w", page, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read releases response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
		}

		var releases []struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &releases); err != nil {
			return nil, fmt.Errorf("failed to decode releases: %w", err)
		}

		for _, r := range releases {
			// Only include client releases tagged as client/vX.Y.Z.
			if after, ok := strings.CutPrefix(r.TagName, "client/"); ok {
				allTags = append(allTags, after) // e.g. "v0.10.0"
			}
		}

		if len(releases) < 100 {
			break
		}
	}
	return allTags, nil
}

// InstallCLIVersionFromGitHub downloads and installs a specific CLI version from GitHub releases,
// placing the binary at /usr/local/bin/doublezero-{version} in the container.
//
// For testnet and devnet environments, the Linux tar.gz archive is downloaded and the doublezero
// binary is extracted. For mainnet-beta, the mainnet-beta deb package is installed with
// dpkg --force-depends (skipping the doublezero-solana dependency which is already satisfied).
func InstallCLIVersionFromGitHub(ctx context.Context, execFn func(ctx context.Context, command []string) ([]byte, error), version, env, githubToken string) error {
	if env == "mainnet-beta" {
		return installCLIMainnetBetaFromGitHub(ctx, execFn, version, githubToken)
	}
	return installCLIFromTarGz(ctx, execFn, version, githubToken)
}

func installCLIFromTarGz(ctx context.Context, execFn func(ctx context.Context, command []string) ([]byte, error), version, githubToken string) error {
	url := fmt.Sprintf(
		"https://github.com/malbeclabs/doublezero/releases/download/client%%2Fv%s/doublezero_Linux_x86_64.tar.gz",
		version)
	script := fmt.Sprintf(`
		set -euo pipefail
		if [ -n "${GH_TOKEN:-}" ]; then
			curl -fsSL -H "Authorization: Bearer $GH_TOKEN" '%s' -o /tmp/doublezero-%s.tar.gz
		else
			curl -fsSL '%s' -o /tmp/doublezero-%s.tar.gz
		fi
		EXTRACT_DIR=$(mktemp -d)
		tar -xzf /tmp/doublezero-%s.tar.gz -C "$EXTRACT_DIR"
		BINARY=$(find "$EXTRACT_DIR" -name doublezero -type f | head -1)
		cp "$BINARY" /usr/local/bin/doublezero-%s
		chmod +x /usr/local/bin/doublezero-%s
		rm -rf /tmp/doublezero-%s.tar.gz "$EXTRACT_DIR"
	`, url, version, url, version, version, version, version, version)
	_, err := execFn(ctx, []string{"env", "GH_TOKEN=" + githubToken, "bash", "-c", script})
	if err != nil {
		return fmt.Errorf("failed to install CLI version %s from GitHub: %w", version, err)
	}
	return nil
}

func installCLIMainnetBetaFromGitHub(ctx context.Context, execFn func(ctx context.Context, command []string) ([]byte, error), version, githubToken string) error {
	url := fmt.Sprintf(
		"https://github.com/malbeclabs/doublezero/releases/download/client%%2Fv%s/doublezero-mainnet-beta_%s_amd64.deb",
		version, version)
	// Use dpkg-deb -x to extract the package contents directly, avoiding the
	// postinstall script (which calls deb-systemd-helper, not available in the container).
	script := fmt.Sprintf(`
		set -euo pipefail
		if [ -n "${GH_TOKEN:-}" ]; then
			curl -fsSL -H "Authorization: Bearer $GH_TOKEN" '%s' -o /tmp/doublezero-mainnet-beta-%s.deb
		else
			curl -fsSL '%s' -o /tmp/doublezero-mainnet-beta-%s.deb
		fi
		EXTRACT_DIR=$(mktemp -d)
		dpkg-deb -x /tmp/doublezero-mainnet-beta-%s.deb "$EXTRACT_DIR"
		cp "$EXTRACT_DIR/usr/bin/doublezero" /usr/local/bin/doublezero-%s
		chmod +x /usr/local/bin/doublezero-%s
		rm -rf /tmp/doublezero-mainnet-beta-%s.deb "$EXTRACT_DIR"
	`, url, version, url, version, version, version, version, version)
	_, err := execFn(ctx, []string{"env", "GH_TOKEN=" + githubToken, "bash", "-c", script})
	if err != nil {
		return fmt.Errorf("failed to install CLI version %s from GitHub: %w", version, err)
	}
	return nil
}

// CurrentVersionLabel is the label used for the current branch's CLI in the compatibility matrix.
const CurrentVersionLabel = "current"

// EnumerateCompatibleVersions returns a sorted list of semver version strings for all
// versions between min (inclusive) and current (inclusive), plus "current" for the branch build.
// Each entry must be in the format "v<major>.<minor>.<patch>" except for the final "current" entry.
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
		// Include all released versions from min through current (inclusive).
		if CompareProgramVersions(pv, min) >= 0 && CompareProgramVersions(pv, current) <= 0 {
			versions = append(versions, ver)
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		vi, _ := ParseSemver(versions[i])
		vj, _ := ParseSemver(versions[j])
		return CompareProgramVersions(vi, vj) < 0
	})
	// Always append "current" at the end for the branch build (may not be released yet).
	versions = append(versions, CurrentVersionLabel)
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
