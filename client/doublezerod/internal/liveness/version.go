package liveness

import "fmt"

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
