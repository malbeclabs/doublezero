package enricher

import (
	"net"
	"testing"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

func mustDecodeBase58To32(t *testing.T, s string) [32]byte {
	t.Helper()
	decoded, err := base58.Decode(s)
	require.NoError(t, err, "failed to decode base58 string: %s", s)
	require.Len(t, decoded, 32, "decoded bytes should be 32 bytes")
	var result [32]byte
	copy(result[:], decoded)
	return result
}

func mustParseIP(t *testing.T, s string) [4]uint8 {
	t.Helper()
	ip := net.ParseIP(s).To4()
	require.NotNil(t, ip, "failed to parse IP: %s", s)
	return [4]uint8{ip[0], ip[1], ip[2], ip[3]}
}

func TestServiceabilityAnnotator_Annotate(t *testing.T) {
	const (
		// User 1 (Amsterdam) keys
		user1DevicePK   = "GphgLkA7JDVtkDQZCiDrwrDvaUs8r8XczEae1KkV6CGQ"
		user1FacilityPK = "67E6GKoWXVrHwGoV64sQXUnE2mgvS5tuutq2FXHrD9e1"
		user1MetroPK    = "BKJWUyoW2sJkbenX9PFnBfGWAJ1uQkLbMDh39sG3sqph"
		user1DzIP       = "1.1.1.1"

		// User 2 (Frankfurt) keys
		user2DevicePK   = "2AFsyp34CFTS5UZJpoqYXvyzFnRW49Q5s7xMEtFFEDVm"
		user2FacilityPK = "FQmM1TfBDgKjdTBKauy5fJ3M5b6CZzeddw3vUwqSWTYu"
		user2MetroPK    = "8dvMd6ffPuMEGnaUyvSqu9HEYxyi6yrgMJLazH9xiaGq"
		user2DzIP       = "2.2.2.2"
	)

	device1PK := mustDecodeBase58To32(t, user1DevicePK)
	facility1PK := mustDecodeBase58To32(t, user1FacilityPK)
	metro1PK := mustDecodeBase58To32(t, user1MetroPK)

	device2PK := mustDecodeBase58To32(t, user2DevicePK)
	facility2PK := mustDecodeBase58To32(t, user2FacilityPK)
	metro2PK := mustDecodeBase58To32(t, user2MetroPK)

	programData := serviceability.ProgramData{
		Users: []serviceability.User{
			{
				DzIp:         mustParseIP(t, user1DzIP),
				DevicePubKey: mustDecodeBase58To32(t, user1DevicePK),
			},
			{
				DzIp:         mustParseIP(t, user2DzIP),
				DevicePubKey: mustDecodeBase58To32(t, user2DevicePK),
			},
		},
		Devices: []serviceability.Device{
			{
				PubKey:         device1PK,
				Code:           "ams001-dz002",
				FacilityPubKey: facility1PK,
				MetroPubKey:    metro1PK,
			},
			{
				PubKey:         device2PK,
				Code:           "frankry",
				FacilityPubKey: facility2PK,
				MetroPubKey:    metro2PK,
			},
		},
		Facilities: []serviceability.Facility{
			{
				PubKey: facility1PK,
				Code:   "EQX-AM4",
				Name:   "Amsterdam",
			},
			{
				PubKey: facility2PK,
				Code:   "EQX-FR13",
				Name:   "Frankfurt",
			},
		},
		Metros: []serviceability.Metro{
			{
				PubKey: metro1PK,
				Code:   "ams",
				Name:   "Amsterdam",
			},
			{
				PubKey: metro2PK,
				Code:   "fra",
				Name:   "Frankfurt",
			},
		},
	}

	users := BuildUserMap(&programData)
	devices := BuildDeviceMap(&programData)
	facilities := BuildFacilityMap(&programData)
	metros := BuildMetroMap(&programData)

	annotator := &ServiceabilityAnnotator{
		name:       "serviceability annotator",
		users:      users,
		devices:    devices,
		facilities: facilities,
		metros:     metros,
	}

	flow := &FlowSample{
		SrcAddress: net.ParseIP(user1DzIP).To4(),
		DstAddress: net.ParseIP(user2DzIP).To4(),
	}

	err := annotator.Annotate(flow)
	require.NoError(t, err)

	require.Equal(t, "ams001-dz002", flow.SrcDeviceCode, "SrcDeviceCode should be ams001-dz002")
	require.Equal(t, "frankry", flow.DstDeviceCode, "DstDeviceCode should be frankry")
	require.Equal(t, "EQX-AM4", flow.SrcFacility, "SrcFacility should be EQX-AM4")
	require.Equal(t, "EQX-FR13", flow.DstFacility, "DstFacility should be EQX-FR13")
	require.Equal(t, "ams", flow.SrcMetro, "SrcMetro should be ams")
	require.Equal(t, "fra", flow.DstMetro, "DstMetro should be fra")
}
