package liveness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClient_Liveness_ClientVersionChannel_String_KnownValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		ch   ClientVersionChannel
		want string
	}{
		{"stable", VersionChannelStable, ""},
		{"alpha", VersionChannelAlpha, "-alpha"},
		{"beta", VersionChannelBeta, "-beta"},
		{"rc", VersionChannelRC, "-rc"},
		{"dev", VersionChannelDev, "-dev"},
		{"other", VersionChannelOther, "-other"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.ch.String())
		})
	}
}

func TestClient_Liveness_ClientVersionChannel_String_UnknownValue(t *testing.T) {
	t.Parallel()

	ch := ClientVersionChannel(250)
	require.Equal(t, "-unknown(250)", ch.String())
}

func TestClient_Liveness_ClientVersion_String(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		v    ClientVersion
		want string
	}{
		{"stable", ClientVersion{Major: 1, Minor: 2, Patch: 3, Channel: VersionChannelStable}, "1.2.3"},
		{"alpha", ClientVersion{Major: 1, Minor: 0, Patch: 0, Channel: VersionChannelAlpha}, "1.0.0-alpha"},
		{"beta", ClientVersion{Major: 0, Minor: 1, Patch: 5, Channel: VersionChannelBeta}, "0.1.5-beta"},
		{"rc", ClientVersion{Major: 9, Minor: 9, Patch: 9, Channel: VersionChannelRC}, "9.9.9-rc"},
		{"dev", ClientVersion{Major: 2, Minor: 3, Patch: 4, Channel: VersionChannelDev}, "2.3.4-dev"},
		{"other", ClientVersion{Major: 3, Minor: 4, Patch: 5, Channel: VersionChannelOther}, "3.4.5-other"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.v.String())
		})
	}
}

func TestClient_Liveness_ParseClientVersion_Success(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		maj     uint8
		min     uint8
		patch   uint8
		channel ClientVersionChannel
	}{
		{"stable", "1.2.3", 1, 2, 3, VersionChannelStable},
		{"alpha", "1.2.3-alpha", 1, 2, 3, VersionChannelAlpha},
		{"beta", "1.2.3-beta", 1, 2, 3, VersionChannelBeta},
		{"rc", "1.2.3-rc", 1, 2, 3, VersionChannelRC},
		{"dev", "1.2.3-dev", 1, 2, 3, VersionChannelDev},
		{"otherSuffix", "1.2.3-foo", 1, 2, 3, VersionChannelOther},
		{"otherWithHyphen", "1.2.3-foo-bar", 1, 2, 3, VersionChannelOther},
		{"maxValues", "255.255.255-dev", 255, 255, 255, VersionChannelDev},
		{
			name:    "gitSuffixDevFromMetadata",
			in:      "0.8.1~git20251210140934.6dc3cef6",
			maj:     0,
			min:     8,
			patch:   1,
			channel: VersionChannelDev,
		},
		{
			name:    "gitSuffixDevExplicit",
			in:      "0.8.1-dev~git20251210140934.6dc3cef6",
			maj:     0,
			min:     8,
			patch:   1,
			channel: VersionChannelDev,
		},
		{
			name:    "buildMetadataPlus",
			in:      "1.2.3+build.1",
			maj:     1,
			min:     2,
			patch:   3,
			channel: VersionChannelStable,
		},
		{
			name:    "tildeMetadata",
			in:      "1.2.3~edge",
			maj:     1,
			min:     2,
			patch:   3,
			channel: VersionChannelStable,
		},
		{
			name:    "devWithPlusMetadata",
			in:      "1.2.3-dev+meta",
			maj:     1,
			min:     2,
			patch:   3,
			channel: VersionChannelDev,
		},
		{
			name:    "alphaDotExtended",
			in:      "1.2.3-alpha.1",
			maj:     1,
			min:     2,
			patch:   3,
			channel: VersionChannelAlpha,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseClientVersion(tc.in)
			require.NoError(t, err)

			require.Equal(t, tc.maj, got.Major)
			require.Equal(t, tc.min, got.Minor)
			require.Equal(t, tc.patch, got.Patch)
			require.Equal(t, tc.channel, got.Channel)
		})
	}
}

func TestClient_Liveness_ParseClientVersion_Invalid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		err  string
	}{
		{"empty", "", "empty version string"},
		{"tooFewParts", "1.2", `invalid version "1.2": expected MAJOR.MINOR.PATCH`},
		{"tooManyParts", "1.2.3.4", `invalid version "1.2.3.4": expected MAJOR.MINOR.PATCH`},
		{"nonNumericMajor", "x.2.3", `invalid major version in "x.2.3"`},
		{"nonNumericMinor", "1.x.3", `invalid minor version in "1.x.3"`},
		{"nonNumericPatch", "1.2.x", `invalid patch version in "1.2.x"`},
		{"majorOutOfRange", "256.0.0", `invalid major version in "256.0.0"`},
		{"minorOutOfRange", "1.256.0", `invalid minor version in "1.256.0"`},
		{"patchOutOfRange", "1.2.256", `invalid patch version in "1.2.256"`},
		{"negativeMajor", "-1.2.3", `invalid version "-1.2.3": expected MAJOR.MINOR.PATCH`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseClientVersion(tc.in)
			require.Error(t, err)
			require.EqualError(t, err, tc.err)
		})
	}
}

func TestClient_Liveness_ClientVersion_RoundTrip_StringAndParse(t *testing.T) {
	t.Parallel()

	cases := []ClientVersion{
		{Major: 0, Minor: 0, Patch: 1, Channel: VersionChannelStable},
		{Major: 1, Minor: 2, Patch: 3, Channel: VersionChannelAlpha},
		{Major: 4, Minor: 5, Patch: 6, Channel: VersionChannelBeta},
		{Major: 7, Minor: 8, Patch: 9, Channel: VersionChannelRC},
		{Major: 10, Minor: 11, Patch: 12, Channel: VersionChannelDev},
		{Major: 13, Minor: 14, Patch: 15, Channel: VersionChannelOther},
	}

	for i, v := range cases {
		v := v
		t.Run(v.String(), func(t *testing.T) {
			t.Parallel()

			s := v.String()
			got, err := ParseClientVersion(s)
			require.NoError(t, err, "case %d", i)
			require.Equal(t, v, got)
		})
	}
}
