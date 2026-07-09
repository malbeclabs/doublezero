package serviceability_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixture-driven tests over the binary fixtures in sdk/serviceability/testdata/fixtures.
// The .bin / .json pairs are produced by the Rust generator at
// sdk/serviceability/testdata/fixtures/generate-fixtures and shared with the
// Python and TypeScript SDKs so the on-disk shape stays in lockstep across
// languages. Regenerate with `make generate-fixtures`.

type fixtureField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"typ"`
}

type fixtureMeta struct {
	Name        string         `json:"name"`
	AccountType uint8          `json:"account_type"`
	Fields      []fixtureField `json:"fields"`
}

func fixturesDir() string {
	_, filename, _, _ := runtime.Caller(0)
	// .../smartcontract/sdk/go/serviceability/fixture_test.go → repo root
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "..", "sdk", "serviceability", "testdata", "fixtures")
}

func loadFixture(t *testing.T, name string) ([]byte, fixtureMeta) {
	t.Helper()
	dir := fixturesDir()
	bin, err := os.ReadFile(filepath.Join(dir, name+".bin"))
	require.NoErrorf(t, err, "reading %s.bin", name)
	jsonBytes, err := os.ReadFile(filepath.Join(dir, name+".json"))
	require.NoErrorf(t, err, "reading %s.json", name)
	var meta fixtureMeta
	require.NoErrorf(t, json.Unmarshal(jsonBytes, &meta), "parsing %s.json", name)
	return bin, meta
}

// expectedInterfaceSize recomputes the on-disk byte length of an Interface
// element so tests don't bake magic numbers. Layout matches Rust
// Interface::serialize_body (interface.rs:641-658): u16 size + u8 version
// (3-byte prefix) + u8 status + (u32+len) name + 4*u8 + u64*2 + u16 + u8 + u16
// + 5-byte ip_net + u16 + u8 + (u32+34*N) flex_algo_node_segments.
func expectedInterfaceSize(ni serviceability.Interface) uint16 {
	body := 1 + (4 + len(ni.Name)) + 1 + 1 + 1 + 1 + 8 + 8 + 2 + 1 + 2 + 5 + 2 + 1 + (4 + 34*len(ni.FlexAlgoNodeSegments))
	return uint16(3 + body)
}

func TestFixtureDevice(t *testing.T) {
	data, meta := loadFixture(t, "device")
	require.Equal(t, "Device", meta.Name)
	require.Equal(t, uint8(serviceability.DeviceType), meta.AccountType)

	var dev serviceability.Device
	r := serviceability.NewByteReader(data)
	serviceability.DeserializeDevice(r, &dev)
	require.NoError(t, dev.DeserializeError)

	assert.Equal(t, serviceability.DeviceType, dev.AccountType)
	assert.Equal(t, "dz1", dev.Code)
	assert.Equal(t, "mgmt", dev.MgmtVrf)
	assert.Equal(t, [4]byte{203, 0, 113, 1}, dev.PublicIp)

	// Legacy slot is the V2 projection of new_interfaces (always V2 per #3653);
	// both entries carry version 1 and no FlexAlgoNodeSegments.
	require.Len(t, dev.DeprecatedInterfaces, 2)
	assert.Equal(t, uint8(1), dev.DeprecatedInterfaces[0].Version)
	assert.Equal(t, "Loopback0", dev.DeprecatedInterfaces[0].Name)
	assert.Equal(t, serviceability.LoopbackTypeVpnv4, dev.DeprecatedInterfaces[0].LoopbackType)
	assert.Empty(t, dev.DeprecatedInterfaces[0].FlexAlgoNodeSegments)
	assert.Equal(t, uint8(1), dev.DeprecatedInterfaces[1].Version)
	assert.Equal(t, "Ethernet1", dev.DeprecatedInterfaces[1].Name)
	assert.True(t, dev.DeprecatedInterfaces[1].UserTunnelEndpoint)

	// Trailing new_interfaces vec carries the full V4 NewInterface bodies.
	require.Len(t, dev.Interfaces, 2)
	ni0 := dev.Interfaces[0]
	assert.Equal(t, uint8(serviceability.CurrentInterfaceVersion), ni0.Version)
	assert.Equal(t, "Loopback0", ni0.Name)
	assert.Equal(t, serviceability.LoopbackTypeVpnv4, ni0.LoopbackType)
	require.Len(t, ni0.FlexAlgoNodeSegments, 1)
	assert.Equal(t, uint16(300), ni0.FlexAlgoNodeSegments[0].NodeSegmentIdx)
	assert.Equal(t, expectedInterfaceSize(ni0), ni0.Size)

	ni1 := dev.Interfaces[1]
	assert.Equal(t, uint8(serviceability.CurrentInterfaceVersion), ni1.Version)
	assert.Equal(t, "Ethernet1", ni1.Name)
	assert.True(t, ni1.UserTunnelEndpoint)
	assert.Empty(t, ni1.FlexAlgoNodeSegments)
	assert.Equal(t, expectedInterfaceSize(ni1), ni1.Size)
}

// Pre-#3667 on-disk format: legacy `interfaces` vec only, no trailing
// `new_interfaces`. SDK rebuilds new_interfaces from the legacy vec, stamping
// each entry with Version=CurrentInterfaceVersion and Size=0.
func TestFixtureDeviceLegacy(t *testing.T) {
	data, meta := loadFixture(t, "device_legacy")
	require.Equal(t, "DeviceLegacy", meta.Name)

	var dev serviceability.Device
	r := serviceability.NewByteReader(data)
	serviceability.DeserializeDevice(r, &dev)
	require.NoError(t, dev.DeserializeError)

	// Legacy slot mirrors the original V1+V2 hand-serialized shape.
	require.Len(t, dev.DeprecatedInterfaces, 2)
	assert.Equal(t, uint8(0), dev.DeprecatedInterfaces[0].Version) // V1
	assert.Equal(t, "Loopback0", dev.DeprecatedInterfaces[0].Name)
	assert.Equal(t, uint8(1), dev.DeprecatedInterfaces[1].Version) // V2
	assert.Equal(t, "Ethernet1", dev.DeprecatedInterfaces[1].Name)

	// Rebuilt new_interfaces: same field values as the legacy entries, but
	// stamped with the current schema version and zero on-disk size.
	require.Len(t, dev.Interfaces, 2)
	for _, ni := range dev.Interfaces {
		assert.Equal(t, uint8(serviceability.CurrentInterfaceVersion), ni.Version)
		assert.Equal(t, uint16(0), ni.Size)
		assert.Empty(t, ni.FlexAlgoNodeSegments)
	}
	assert.Equal(t, "Loopback0", dev.Interfaces[0].Name)
	assert.Equal(t, serviceability.LoopbackTypeVpnv4, dev.Interfaces[0].LoopbackType)
	assert.Equal(t, "Ethernet1", dev.Interfaces[1].Name)
	assert.True(t, dev.Interfaces[1].UserTunnelEndpoint)
}

// Same on-disk shape as device.bin, but the last trailing-vec element is
// doctored with Version=5 and 8 trailing junk bytes. SDK reads the known body
// fields then advances to start+size, skipping the junk.
func TestFixtureDeviceFutureVersion(t *testing.T) {
	data, meta := loadFixture(t, "device_future_version")
	require.Equal(t, "DeviceFutureVersion", meta.Name)

	var dev serviceability.Device
	r := serviceability.NewByteReader(data)
	serviceability.DeserializeDevice(r, &dev)
	require.NoError(t, dev.DeserializeError)

	require.Len(t, dev.Interfaces, 2)
	ni0 := dev.Interfaces[0]
	assert.Equal(t, uint8(serviceability.CurrentInterfaceVersion), ni0.Version)
	assert.Equal(t, "Loopback0", ni0.Name)
	require.Len(t, ni0.FlexAlgoNodeSegments, 1)

	// Doctored element: future version stamp + 8 trailing junk bytes the reader
	// skips via seek(start+size).
	ni1 := dev.Interfaces[1]
	assert.Equal(t, uint8(5), ni1.Version)
	assert.Equal(t, expectedInterfaceSize(ni1)+8, ni1.Size)
	assert.Equal(t, "Ethernet1", ni1.Name)
	assert.True(t, ni1.UserTunnelEndpoint)
}

func TestFixtureAccessPass(t *testing.T) {
	data, meta := loadFixture(t, "access_pass")
	require.Equal(t, "AccessPass", meta.Name)

	var ap serviceability.AccessPass
	serviceability.DeserializeAccessPass(serviceability.NewByteReader(data), &ap)

	assert.Equal(t, serviceability.AccessPassType, ap.AccountType)
	assert.Equal(t, uint8(244), ap.BumpSeed)
	assert.Equal(t, serviceability.AccessPassTypePrepaid, ap.AccessPassTypeTag)
	assert.Equal(t, uint16(3), ap.ConnectionCount)
	assert.Equal(t, uint8(1), ap.Flags)
	assert.Empty(t, ap.TenantAllowlist)
	assert.Equal(t, uint16(2), ap.UnicastUserCount)
	assert.Equal(t, uint16(4), ap.MaxUnicastUsers)
	assert.Equal(t, uint16(1), ap.MulticastUserCount)
	assert.Equal(t, uint16(3), ap.MaxMulticastUsers)
}

func TestFixtureAccessPassValidator(t *testing.T) {
	data, meta := loadFixture(t, "access_pass_validator")
	require.Equal(t, "AccessPassValidator", meta.Name)

	var ap serviceability.AccessPass
	serviceability.DeserializeAccessPass(serviceability.NewByteReader(data), &ap)

	assert.Equal(t, serviceability.AccessPassTypeSolanaValidator, ap.AccessPassTypeTag)
	assert.NotEqual(t, [32]byte{}, ap.AssociatedPubkey)
	assert.Equal(t, uint16(1), ap.ConnectionCount)
	assert.Equal(t, uint8(3), ap.Flags)
	assert.Equal(t, uint16(0), ap.UnicastUserCount)
	assert.Equal(t, uint16(5), ap.MaxUnicastUsers)
	assert.Equal(t, uint16(0), ap.MulticastUserCount)
	assert.Equal(t, uint16(2), ap.MaxMulticastUsers)
}

func TestFixtureAccessPassEdgeSeat(t *testing.T) {
	data, meta := loadFixture(t, "access_pass_edge_seat")
	require.Equal(t, "AccessPassEdgeSeat", meta.Name)

	var ap serviceability.AccessPass
	serviceability.DeserializeAccessPass(serviceability.NewByteReader(data), &ap)

	// EdgeSeat is Rust discriminant 4 and now carries a Vec<FeedSeat> payload.
	assert.Equal(t, serviceability.AccessPassTypeEdgeSeat, ap.AccessPassTypeTag)
	assert.Equal(t, [32]byte{}, ap.AssociatedPubkey)
	require.Len(t, ap.FeedSeats, 1)
	assert.Equal(t, byte(0xB2), ap.FeedSeats[0].FeedKey[0])
	assert.Equal(t, uint8(7), ap.FeedSeats[0].MaxUsers)
	assert.Equal(t, uint8(4), ap.FeedSeats[0].MaxFutureUsers)
	assert.Equal(t, uint8(3), ap.FeedSeats[0].CurrentUsers)
	assert.Equal(t, uint8(15), ap.FeedSeats[0].AnniversaryDay)
	assert.Equal(t, int64(1800000000), ap.FeedSeats[0].WindowEnd)
	assert.Equal(t, int64(1900000000), ap.FeedSeats[0].TerminatesAt)
	assert.Equal(t, uint8(2), ap.Flags) // ALLOW_MULTIPLE_IP
	assert.Equal(t, uint16(2), ap.UnicastUserCount)
	assert.Equal(t, uint16(4), ap.MaxUnicastUsers)
	assert.Equal(t, uint16(1), ap.MulticastUserCount)
	assert.Equal(t, uint16(3), ap.MaxMulticastUsers)
}

func TestFixtureFeed(t *testing.T) {
	data, meta := loadFixture(t, "feed")
	require.Equal(t, "Feed", meta.Name)

	var feed serviceability.Feed
	serviceability.DeserializeFeed(serviceability.NewByteReader(data), &feed)

	assert.Equal(t, serviceability.FeedType, feed.AccountType)
	assert.Equal(t, byte(0xE0), feed.Owner[0])
	assert.Equal(t, uint8(239), feed.BumpSeed)
	assert.Equal(t, "shreds", feed.Code)
	assert.Equal(t, "Shreds", feed.Name)
	assert.Equal(t, byte(0xE1), feed.Exchange[0])
	require.Len(t, feed.Groups, 2)
	assert.Equal(t, byte(0xE2), feed.Groups[0][0])
	assert.Equal(t, byte(0xE3), feed.Groups[1][0])
}

// A pre-migration account (lacking the 8 trailing cap bytes) decodes with counts 0 and caps 1,
// matching the Rust program's TryFrom unwrap_or defaults.
func TestFixtureAccessPassLegacyCapDefaults(t *testing.T) {
	data, _ := loadFixture(t, "access_pass")
	legacy := data[:len(data)-8] // drop the four u16 cap fields

	var ap serviceability.AccessPass
	serviceability.DeserializeAccessPass(serviceability.NewByteReader(legacy), &ap)

	assert.Equal(t, uint16(0), ap.UnicastUserCount)
	assert.Equal(t, uint16(1), ap.MaxUnicastUsers)
	assert.Equal(t, uint16(0), ap.MulticastUserCount)
	assert.Equal(t, uint16(1), ap.MaxMulticastUsers)
}
