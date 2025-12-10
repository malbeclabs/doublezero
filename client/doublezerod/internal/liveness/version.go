package liveness

import (
	"fmt"
	"strconv"
	"strings"
)

// ClientVersionChannel represents a coarse "release channel" for the build.
// This is encoded as a single byte in the ControlPacket wire format.
// Only small, stable changes should be made here.
type ClientVersionChannel uint8

const (
	VersionChannelStable ClientVersionChannel = iota
	VersionChannelAlpha
	VersionChannelBeta
	VersionChannelRC
	VersionChannelDev
	VersionChannelOther
)

// String returns the semver-compatible suffix for the channel.
// Example: Alpha → "-alpha".
func (ch ClientVersionChannel) String() string {
	switch ch {
	case VersionChannelStable:
		return ""
	case VersionChannelAlpha:
		return "-alpha"
	case VersionChannelBeta:
		return "-beta"
	case VersionChannelRC:
		return "-rc"
	case VersionChannelDev:
		return "-dev"
	case VersionChannelOther:
		return "-other"
	default:
		return fmt.Sprintf("-unknown(%d)", uint8(ch))
	}
}

// ClientVersion encodes the semver-like build version of the peer.
//
// IMPORTANT: This structure is serialized directly into the 40-byte
// ControlPacket wire format (bytes 21–24). Any change to its size,
// ordering, or meaning **changes the on-wire protocol**. Update with care.
type ClientVersion struct {
	Major   uint8                // Semver major version
	Minor   uint8                // Semver minor version
	Patch   uint8                // Semver patch version
	Channel ClientVersionChannel // Pre-release / dev channel indicator
}

// String returns a semver-like string (e.g. "1.2.3-dev").
func (v ClientVersion) String() string {
	return fmt.Sprintf("%d.%d.%d%s",
		v.Major, v.Minor, v.Patch, v.Channel.String())
}

// ParseClientVersion parses a semver-like string (e.g. "1.2.3-dev") into a ClientVersion.
func ParseClientVersion(s string) (ClientVersion, error) {
	var v ClientVersion

	s = strings.TrimSpace(s)
	if s == "" {
		return v, fmt.Errorf("empty version string")
	}

	// Split off channel-style suffix: "1.2.3-dev" → ["1.2.3", "dev"]
	parts := strings.SplitN(s, "-", 2)

	// Numeric part may contain extra metadata: "0.8.1~git..." or "1.2.3+build.1".
	// Strip anything after '~' or '+' before parsing MAJOR.MINOR.PATCH.
	rawNumeric := parts[0]
	numeric := rawNumeric
	meta := ""
	if idx := strings.IndexAny(rawNumeric, "~+"); idx != -1 {
		numeric = rawNumeric[:idx]
		if idx+1 < len(rawNumeric) {
			meta = rawNumeric[idx+1:]
		}
	}

	nums := strings.Split(numeric, ".")
	if len(nums) != 3 {
		return v, fmt.Errorf("invalid version %q: expected MAJOR.MINOR.PATCH", s)
	}

	maj, err := strconv.Atoi(nums[0])
	if err != nil || maj < 0 || maj > 255 {
		return v, fmt.Errorf("invalid major version in %q", s)
	}
	min, err := strconv.Atoi(nums[1])
	if err != nil || min < 0 || min > 255 {
		return v, fmt.Errorf("invalid minor version in %q", s)
	}
	pat, err := strconv.Atoi(nums[2])
	if err != nil || pat < 0 || pat > 255 {
		return v, fmt.Errorf("invalid patch version in %q", s)
	}

	ch := VersionChannelStable
	if len(parts) == 2 {
		suffix := parts[1]

		// Trim any extra metadata after the channel: "dev+meta", "dev~git...", "alpha.1", etc.
		if idx := strings.IndexAny(suffix, "+~."); idx != -1 {
			suffix = suffix[:idx]
		}
		switch strings.ToLower(suffix) {
		case "alpha":
			ch = VersionChannelAlpha
		case "beta":
			ch = VersionChannelBeta
		case "rc":
			ch = VersionChannelRC
		case "dev":
			ch = VersionChannelDev
		default:
			ch = VersionChannelOther
		}
	}

	// If we didn't see an explicit channel, but the metadata clearly indicates
	// a git/edge build (e.g. "0.8.1~git2025..."), classify as dev.
	if ch == VersionChannelStable {
		if strings.Contains(strings.ToLower(meta), "git") {
			ch = VersionChannelDev
		}
	}

	v.Major = uint8(maj)
	v.Minor = uint8(min)
	v.Patch = uint8(pat)
	v.Channel = ch
	return v, nil
}
