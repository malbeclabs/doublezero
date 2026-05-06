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

// expectedNewInterfaceSize recomputes the on-disk byte length of a NewInterface
// element so tests don't bake magic numbers. Layout matches Rust
// NewInterface::serialize_body (interface.rs:641-658): u16 size + u8 version
// (3-byte prefix) + u8 status + (u32+len) name + 4*u8 + u64*2 + u16 + u8 + u16
// + 5-byte ip_net + u16 + u8 + (u32+34*N) flex_algo_node_segments.
func expectedNewInterfaceSize(ni serviceability.Interface) uint16 {
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
	require.Len(t, dev.Interfaces, 2)
	assert.Equal(t, uint8(1), dev.Interfaces[0].Version)
	assert.Equal(t, "Loopback0", dev.Interfaces[0].Name)
	assert.Equal(t, serviceability.LoopbackTypeVpnv4, dev.Interfaces[0].LoopbackType)
	assert.Empty(t, dev.Interfaces[0].FlexAlgoNodeSegments)
	assert.Equal(t, uint8(1), dev.Interfaces[1].Version)
	assert.Equal(t, "Ethernet1", dev.Interfaces[1].Name)
	assert.True(t, dev.Interfaces[1].UserTunnelEndpoint)

	// Trailing new_interfaces vec carries the full V4 NewInterface bodies.
	require.Len(t, dev.NewInterfaces, 2)
	ni0 := dev.NewInterfaces[0]
	assert.Equal(t, uint8(serviceability.CurrentInterfaceVersion), ni0.Version)
	assert.Equal(t, "Loopback0", ni0.Name)
	assert.Equal(t, serviceability.LoopbackTypeVpnv4, ni0.LoopbackType)
	require.Len(t, ni0.FlexAlgoNodeSegments, 1)
	assert.Equal(t, uint16(300), ni0.FlexAlgoNodeSegments[0].NodeSegmentIdx)
	assert.Equal(t, expectedNewInterfaceSize(ni0), ni0.Size)

	ni1 := dev.NewInterfaces[1]
	assert.Equal(t, uint8(serviceability.CurrentInterfaceVersion), ni1.Version)
	assert.Equal(t, "Ethernet1", ni1.Name)
	assert.True(t, ni1.UserTunnelEndpoint)
	assert.Empty(t, ni1.FlexAlgoNodeSegments)
	assert.Equal(t, expectedNewInterfaceSize(ni1), ni1.Size)
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
	require.Len(t, dev.Interfaces, 2)
	assert.Equal(t, uint8(0), dev.Interfaces[0].Version) // V1
	assert.Equal(t, "Loopback0", dev.Interfaces[0].Name)
	assert.Equal(t, uint8(1), dev.Interfaces[1].Version) // V2
	assert.Equal(t, "Ethernet1", dev.Interfaces[1].Name)

	// Rebuilt new_interfaces: same field values as the legacy entries, but
	// stamped with the current schema version and zero on-disk size.
	require.Len(t, dev.NewInterfaces, 2)
	for _, ni := range dev.NewInterfaces {
		assert.Equal(t, uint8(serviceability.CurrentInterfaceVersion), ni.Version)
		assert.Equal(t, uint16(0), ni.Size)
		assert.Empty(t, ni.FlexAlgoNodeSegments)
	}
	assert.Equal(t, "Loopback0", dev.NewInterfaces[0].Name)
	assert.Equal(t, serviceability.LoopbackTypeVpnv4, dev.NewInterfaces[0].LoopbackType)
	assert.Equal(t, "Ethernet1", dev.NewInterfaces[1].Name)
	assert.True(t, dev.NewInterfaces[1].UserTunnelEndpoint)
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

	require.Len(t, dev.NewInterfaces, 2)
	ni0 := dev.NewInterfaces[0]
	assert.Equal(t, uint8(serviceability.CurrentInterfaceVersion), ni0.Version)
	assert.Equal(t, "Loopback0", ni0.Name)
	require.Len(t, ni0.FlexAlgoNodeSegments, 1)

	// Doctored element: future version stamp + 8 trailing junk bytes the reader
	// skips via seek(start+size).
	ni1 := dev.NewInterfaces[1]
	assert.Equal(t, uint8(5), ni1.Version)
	assert.Equal(t, expectedNewInterfaceSize(ni1)+8, ni1.Size)
	assert.Equal(t, "Ethernet1", ni1.Name)
	assert.True(t, ni1.UserTunnelEndpoint)
}
