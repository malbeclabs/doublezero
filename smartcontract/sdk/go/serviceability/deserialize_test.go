package serviceability_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deviceBuilder assembles a Borsh-style Device byte stream for tests.
//
// Wire format mirrors smartcontract/programs/doublezero-serviceability::state::device.
// Field order through max_multicast_publishers, then optionally a trailing
// new_interfaces vec where each element is laid out as:
//
//	u16 size (incl. 3-byte prefix) | u8 version | body
//
// Body layout matches Rust NewInterface::serialize_body — see that function for
// the canonical ordering.
type deviceBuilder struct {
	buf bytes.Buffer
}

func (b *deviceBuilder) writeU8(v uint8)   { b.buf.WriteByte(v) }
func (b *deviceBuilder) writeBool(v bool)  { b.writeU8(map[bool]uint8{false: 0, true: 1}[v]) }
func (b *deviceBuilder) writeU16(v uint16) { _ = binary.Write(&b.buf, binary.LittleEndian, v) }
func (b *deviceBuilder) writeU32(v uint32) { _ = binary.Write(&b.buf, binary.LittleEndian, v) }
func (b *deviceBuilder) writeU64(v uint64) { _ = binary.Write(&b.buf, binary.LittleEndian, v) }
func (b *deviceBuilder) writeBytes(v []byte) {
	b.buf.Write(v)
}
func (b *deviceBuilder) writeU128(highLow [2]uint64) {
	// Matches ByteReader.ReadU128 in this package: high bytes first, then low.
	b.writeU64(highLow[0])
	b.writeU64(highLow[1])
}
func (b *deviceBuilder) writeString(s string) {
	b.writeU32(uint32(len(s)))
	b.buf.WriteString(s)
}
func (b *deviceBuilder) writePubkey(p [32]byte) { b.buf.Write(p[:]) }
func (b *deviceBuilder) writeIPv4(p [4]byte)    { b.buf.Write(p[:]) }
func (b *deviceBuilder) writeNetworkV4(p [5]byte) {
	b.buf.Write(p[:])
}

// writeDeviceHeader writes everything from account_type through mgmt_vrf
// using zero/default values, then runs an explicit interface count + body
// callback before writing the trailing scalar/vec fields.
func (b *deviceBuilder) writeDevice(numIfaces uint32, writeIfaces func(*deviceBuilder), trailing func(*deviceBuilder)) {
	var zeroPK [32]byte
	b.writeU8(uint8(serviceability.DeviceType)) // account_type
	b.writePubkey(zeroPK)                       // owner
	b.writeU128([2]uint64{0, 1})                // index
	b.writeU8(255)                              // bump_seed
	b.writePubkey(zeroPK)                       // location_pk
	b.writePubkey(zeroPK)                       // exchange_pk
	b.writeU8(0)                                // device_type
	b.writeIPv4([4]byte{1, 2, 3, 4})            // public_ip
	b.writeU8(1)                                // status (Activated)
	b.writeString("dev-test")                   // code
	b.writeU32(0)                               // dz_prefixes (empty)
	b.writePubkey(zeroPK)                       // metrics_publisher_pk
	b.writePubkey(zeroPK)                       // contributor_pk
	b.writeString("default")                    // mgmt_vrf

	b.writeU32(numIfaces)
	if writeIfaces != nil {
		writeIfaces(b)
	}

	b.writeU32(0) // reference_count
	b.writeU16(0) // users_count
	b.writeU16(0) // max_users
	b.writeU8(0)  // device_health (Unknown)
	b.writeU8(0)  // device_desired_status (Pending)
	b.writeU16(0) // unicast_users_count
	b.writeU16(0) // multicast_subscribers_count
	b.writeU16(0) // max_unicast_users
	b.writeU16(0) // max_multicast_subscribers
	b.writeU16(0) // reserved_seats
	b.writeU16(0) // multicast_publishers_count
	b.writeU16(0) // max_multicast_publishers

	if trailing != nil {
		trailing(b)
	}
}

// writeLegacyInterfaceV2 appends a single legacy enum-encoded V2 interface
// (discriminant 1) with caller-provided name. Other fields are zeroed.
func writeLegacyInterfaceV2(b *deviceBuilder, name string) {
	b.writeU8(1)                // enum disc: V2
	b.writeU8(0)                // status
	b.writeString(name)         // name
	b.writeU8(0)                // interface_type
	b.writeU8(0)                // interface_cyoa
	b.writeU8(0)                // interface_dia
	b.writeU8(0)                // loopback_type
	b.writeU64(0)               // bandwidth
	b.writeU64(0)               // cir
	b.writeU16(0)               // mtu
	b.writeU8(0)                // routing_mode
	b.writeU16(0)               // vlan_id
	b.writeNetworkV4([5]byte{}) // ip_net
	b.writeU16(0)               // node_segment_idx
	b.writeBool(false)          // user_tunnel_endpoint
}

// newInterfaceBody returns the body bytes (no size/version prefix) for a
// minimal V4 NewInterface element with caller-provided name.
func newInterfaceBody(name string) []byte {
	var body bytes.Buffer
	body.WriteByte(0) // status
	_ = binary.Write(&body, binary.LittleEndian, uint32(len(name)))
	body.WriteString(name)
	body.WriteByte(0)                                       // interface_type
	body.WriteByte(0)                                       // interface_cyoa
	body.WriteByte(0)                                       // interface_dia
	body.WriteByte(0)                                       // loopback_type
	_ = binary.Write(&body, binary.LittleEndian, uint64(0)) // bandwidth
	_ = binary.Write(&body, binary.LittleEndian, uint64(0)) // cir
	_ = binary.Write(&body, binary.LittleEndian, uint16(0)) // mtu
	body.WriteByte(0)                                       // routing_mode
	_ = binary.Write(&body, binary.LittleEndian, uint16(0)) // vlan_id
	body.Write(make([]byte, 5))                             // ip_net
	_ = binary.Write(&body, binary.LittleEndian, uint16(0)) // node_segment_idx
	body.WriteByte(0)                                       // user_tunnel_endpoint
	_ = binary.Write(&body, binary.LittleEndian, uint32(0)) // flex_algo_node_segments len = 0
	return body.Bytes()
}

// writeNewInterfaceSized appends a single size-prefixed NewInterface (version 4)
// to the builder. size = 3 + len(body); the size header is included in size.
func writeNewInterfaceSized(b *deviceBuilder, name string) {
	body := newInterfaceBody(name)
	size := uint16(3 + len(body))
	b.writeU16(size)
	b.writeU8(serviceability.CurrentInterfaceVersion) // version = 4
	b.writeBytes(body)
}

func TestDeserializeInterfaceSized_PopulatedTrailingVec(t *testing.T) {
	// Cross-language framing assertion: a NewInterface with empty name has body
	// length = 1+4+1+1+1+1+8+8+2+1+2+5+2+1+4 = 42, so size = 3+42 = 45.
	const expectedSizeEmptyName = 45

	var b deviceBuilder
	b.writeDevice(2,
		func(bb *deviceBuilder) {
			writeLegacyInterfaceV2(bb, "Eth1")
			writeLegacyInterfaceV2(bb, "Lo0")
		},
		func(bb *deviceBuilder) {
			bb.writeU32(2) // new_interfaces vec length
			writeNewInterfaceSized(bb, "Eth1")
			writeNewInterfaceSized(bb, "Lo0")
		},
	)

	r := serviceability.NewByteReader(b.buf.Bytes())
	var dev serviceability.Device
	serviceability.DeserializeDevice(r, &dev)
	require.NoError(t, dev.DeserializeError)

	require.Len(t, dev.Interfaces, 2)
	require.Len(t, dev.NewInterfaces, 2)
	assert.Equal(t, "Eth1", dev.NewInterfaces[0].Name)
	assert.Equal(t, "Lo0", dev.NewInterfaces[1].Name)
	assert.Equal(t, uint8(serviceability.CurrentInterfaceVersion), dev.NewInterfaces[0].Version)

	// The size field on NewInterface includes the 2-byte size + 1-byte version
	// + body. For an empty-flex-segs body with name "Eth1": 4+4+...; verified
	// against the expected-empty-name baseline below.
	emptyBody := newInterfaceBody("")
	assert.Equal(t, expectedSizeEmptyName, 3+len(emptyBody))
	for i := range dev.NewInterfaces {
		expected := uint16(3 + len(newInterfaceBody(dev.NewInterfaces[i].Name)))
		assert.Equal(t, expected, dev.NewInterfaces[i].Size, "size mismatch on element %d", i)
	}
}

func TestDeserializeDevice_LegacyAccountRebuildsNewInterfaces(t *testing.T) {
	var b deviceBuilder
	b.writeDevice(2,
		func(bb *deviceBuilder) {
			writeLegacyInterfaceV2(bb, "Eth1")
			writeLegacyInterfaceV2(bb, "Lo0")
		},
		nil, // no trailing bytes -> legacy fallback
	)

	r := serviceability.NewByteReader(b.buf.Bytes())
	var dev serviceability.Device
	serviceability.DeserializeDevice(r, &dev)
	require.NoError(t, dev.DeserializeError)

	require.Len(t, dev.Interfaces, 2)
	require.Len(t, dev.NewInterfaces, 2)
	assert.Equal(t, "Eth1", dev.NewInterfaces[0].Name)
	assert.Equal(t, "Lo0", dev.NewInterfaces[1].Name)
	// Rebuilt entries are stamped with the current schema version and zero
	// size (callers don't need on-disk size for a rebuild).
	for _, ni := range dev.NewInterfaces {
		assert.Equal(t, uint8(serviceability.CurrentInterfaceVersion), ni.Version)
		assert.Equal(t, uint16(0), ni.Size)
	}
}

func TestDeserializeDevice_TrailingLengthMismatchSetsError(t *testing.T) {
	var b deviceBuilder
	b.writeDevice(2,
		func(bb *deviceBuilder) {
			writeLegacyInterfaceV2(bb, "Eth1")
			writeLegacyInterfaceV2(bb, "Lo0")
		},
		func(bb *deviceBuilder) {
			bb.writeU32(1) // declared 1 new_interface but legacy has 2 -> mismatch
			writeNewInterfaceSized(bb, "Eth1")
		},
	)

	r := serviceability.NewByteReader(b.buf.Bytes())
	var dev serviceability.Device
	serviceability.DeserializeDevice(r, &dev)
	require.Error(t, dev.DeserializeError)
	assert.Contains(t, dev.DeserializeError.Error(), "length 1 != interfaces length 2")
}

func TestDeserializeInterfaceSized_FutureVersionSkipsTrailingBytes(t *testing.T) {
	// Forge an element with version=5 and 8 trailing junk bytes appended past
	// the known body. The reader should advance past start+size and leave the
	// next element readable.
	body := newInterfaceBody("Future1")
	junk := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE}
	full := append(body, junk...)
	size := uint16(3 + len(full))

	var b deviceBuilder
	b.writeDevice(1,
		func(bb *deviceBuilder) {
			writeLegacyInterfaceV2(bb, "Future1")
		},
		func(bb *deviceBuilder) {
			bb.writeU32(1)
			bb.writeU16(size)
			bb.writeU8(5) // version = 5 (future)
			bb.writeBytes(full)
		},
	)

	r := serviceability.NewByteReader(b.buf.Bytes())
	var dev serviceability.Device
	serviceability.DeserializeDevice(r, &dev)
	require.NoError(t, dev.DeserializeError)
	require.Len(t, dev.NewInterfaces, 1)
	assert.Equal(t, uint8(5), dev.NewInterfaces[0].Version)
	assert.Equal(t, size, dev.NewInterfaces[0].Size)
	// Body fields up to known shape are still parsed.
	assert.Equal(t, "Future1", dev.NewInterfaces[0].Name)
}
