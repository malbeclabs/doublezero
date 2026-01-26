package serviceability

import (
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"testing"
)

func TestReadU8(t *testing.T) {
	t.Parallel()

	data := []byte{0x01, 0x02, 0x03}
	reader := NewByteReader(data)

	val := reader.ReadU8()
	if val != 0x01 {
		t.Errorf("ReadU8 returned incorrect value: got %d, expected %d", val, 0x01)
	}

	val = reader.ReadU8()
	if val != 0x02 {
		t.Errorf("ReadU8 returned incorrect value: got %d, expected %d", val, 0x02)
	}

	val = reader.ReadU8()
	if val != 0x03 {
		t.Errorf("ReadU8 returned incorrect value: got %d, expected %d", val, 0x03)
	}

	val = reader.ReadU8()
	if val != 0 {
		t.Errorf("ReadU8 should have returned 0 due to out of bounds, got %d", val)
	}
}

func TestReadU16(t *testing.T) {
	t.Parallel()

	data := []byte{0x01, 0x02, 0x03, 0x04}
	reader := NewByteReader(data)

	val := reader.ReadU16()
	expected := binary.LittleEndian.Uint16([]byte{0x01, 0x02})
	if val != expected {
		t.Errorf("ReadU16 returned incorrect value: got %d, expected %d", val, expected)
	}

	val = reader.ReadU16()
	expected = binary.LittleEndian.Uint16([]byte{0x03, 0x04})
	if val != expected {
		t.Errorf("ReadU16 returned incorrect value: got %d, expected %d", val, expected)
	}

	val = reader.ReadU16()
	if val != 0 {
		t.Errorf("ReadU16 should have returned 0 due to out of bounds, got %d", val)
	}
}

func TestReadU32(t *testing.T) {
	t.Parallel()

	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	reader := NewByteReader(data)

	val := reader.ReadU32()
	expected := binary.LittleEndian.Uint32([]byte{0x01, 0x02, 0x03, 0x04})
	if val != expected {
		t.Errorf("ReadU32 returned incorrect value: got %d, expected %d", val, expected)
	}

	val = reader.ReadU32()
	expected = binary.LittleEndian.Uint32([]byte{0x05, 0x06, 0x07, 0x08})
	if val != expected {
		t.Errorf("ReadU32 returned incorrect value: got %d, expected %d", val, expected)
	}
	val = reader.ReadU32()
	if val != 0 {
		t.Errorf("ReadU32 should have returned 0 due to out of bounds, got %d", val)
	}
}

func TestReadU64(t *testing.T) {
	t.Parallel()

	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16}
	reader := NewByteReader(data)

	val := reader.ReadU64()
	expected := binary.LittleEndian.Uint64([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})
	if val != expected {
		t.Errorf("ReadU64 returned incorrect value: got %d, expected %d", val, expected)
	}

	val = reader.ReadU64()
	expected = binary.LittleEndian.Uint64([]byte{0x09, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16})
	if val != expected {
		t.Errorf("ReadU64 returned incorrect value: got %d, expected %d", val, expected)
	}

	val = reader.ReadU64()
	if val != 0 {
		t.Errorf("ReadU64 should have returned 0 due to out of bounds, got %d", val)
	}
}

func TestReadU128(t *testing.T) {
	t.Parallel()

	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	reader := NewByteReader(data)

	val := reader.ReadU128()
	expected_high := binary.LittleEndian.Uint64(data[0:8])
	expected_low := binary.LittleEndian.Uint64(data[8:16])
	if val.High != expected_high || val.Low != expected_low {
		t.Errorf("ReadU128 returned incorrect value: got %v, expected {%v, %v}", val, expected_high, expected_low)
	}
	val = reader.ReadU128()
	if val.High != 0 || val.Low != 0 {
		t.Errorf("ReadU128 should have returned []byte{} due to out of bounds, got %v", val)
	}
}

func TestReadF64(t *testing.T) {
	t.Parallel()

	data := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x3f} // 1.0
	reader := NewByteReader(data)

	val := reader.ReadF64()
	if val != 1.0 {
		t.Errorf("ReadF64 returned incorrect value: got %f, expected %f", val, 1.0)
	}
}

func TestReadPubkey(t *testing.T) {
	t.Parallel()

	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33}
	reader := NewByteReader(data)
	val := reader.ReadPubkey()
	expected := [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	if val != expected {
		t.Errorf("ReadPubkey returned incorrect value: got %v, expected %v", val, expected)
	}
	val = reader.ReadPubkey()
	expected2 := [32]byte{}
	if val != expected2 {
		t.Errorf("ReadPubkey returned incorrect value: got %v, expected %v", val, expected2)
	}
}

func TestReadPubkeySlice(t *testing.T) {
	t.Parallel()

	data := []byte{0x02, 0x00, 0x00, 0x00, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64}
	reader := NewByteReader(data)
	val := reader.ReadPubkeySlice()
	expected := [][32]byte{
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},          //nolint
		{33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64}, //nolint
	}
	if !reflect.DeepEqual(val, expected) {
		t.Errorf("ReadPubkeySlice returned incorrect value: got %#v, expected %#v", val, expected)
	}
}

func TestReadIPv4(t *testing.T) {
	t.Parallel()

	data := []byte{1, 2, 3, 4, 5}
	reader := NewByteReader(data)
	val := reader.ReadIPv4()
	expected := [4]byte{1, 2, 3, 4}
	if val != expected {
		t.Errorf("ReadIPv4 returned incorrect value: got %v, expected %v", val, expected)
	}

	val = reader.ReadIPv4()
	expected2 := [4]byte{}
	if val != expected2 {
		t.Errorf("ReadIPv4 should have returned zero array, got %v", val)
	}
}

func TestReadIPv4Slice(t *testing.T) {
	t.Parallel()

	data := []byte{0x02, 0x00, 0x00, 0x00, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	reader := NewByteReader(data)
	val := reader.ReadIPv4Slice()
	expected := [][4]byte{
		{1, 2, 3, 4}, // nolint
		{5, 6, 7, 8}, // nolint
	}
	if !reflect.DeepEqual(val, expected) {
		t.Errorf("ReadNetworkV4Slice returned incorrect value: got %#v, expected %#v", val, expected)
	}
}

func TestReadNetworkV4(t *testing.T) {
	t.Parallel()

	data := []byte{1, 2, 3, 4, 5, 6}
	reader := NewByteReader(data)
	val := reader.ReadNetworkV4()
	expected := [5]byte{1, 2, 3, 4, 5}
	if val != expected {
		t.Errorf("ReadNetworkV4 returned incorrect value: got %v, expected %v", val, expected)
	}
	val = reader.ReadNetworkV4()
	expected2 := [5]byte{}
	if val != expected2 {
		t.Errorf("ReadNetworkV4 should have returned zero array, got %v", val)
	}
}

func TestReadNetworkV4Slice(t *testing.T) {
	t.Parallel()

	data := []byte{0x02, 0x00, 0x00, 0x00, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	reader := NewByteReader(data)
	val := reader.ReadNetworkV4Slice()
	expected := [][5]byte{
		{1, 2, 3, 4, 5},  // nolint
		{6, 7, 8, 9, 10}, // nolint
	}
	if !reflect.DeepEqual(val, expected) {
		t.Errorf("ReadNetworkV4Slice returned incorrect value: got %#v, expected %#v", val, expected)
	}
}

func TestReadString(t *testing.T) {
	t.Parallel()

	data := []byte{0x04, 0x00, 0x00, 0x00, 0x48, 0x65, 0x6c, 0x6c, 0x05}
	reader := NewByteReader(data)
	val := reader.ReadString()
	expected := "Hell"
	if val != expected {
		t.Errorf("ReadString returned incorrect value: got %s, expected %s", val, expected)
	}

	val = reader.ReadString()
	if val != "" {
		t.Errorf("ReadString should have returned empty string, got %s", val)
	}
}

// TestDeserializeInterfaceV2CrossLanguage tests deserializing bytes that Rust produces.
// To get the expected bytes, run in Rust:
//
//	cargo test test_interface_v2_serialization_bytes -- --nocapture
//
// Then copy the hex output here.
func TestDeserializeInterfaceV2CrossLanguage(t *testing.T) {
	t.Parallel()

	// These bytes are ACTUAL output from Rust test:
	// Hex: [01, 03, 0b, 00, 00, 00, 4c, 6f, 6f, 70, 62, 61, 63, 6b, 31, 30, 36, 01, 00, 00, 02, 00, 00, 00, 00, 00, 00, 00, 00, 00, 00, 00, 00, 00, 00, 00, 00, 28, 23, 00, 00, 00, cb, 00, 71, 28, 20, 00, 00, 01]
	//
	// Field breakdown from Rust:
	//   [0] enum discriminant (V2=1): 01
	//   [1] status (Activated=3): 03
	//   [2-5] name length: 11 (0x0000000b)
	//   [6-16] name: "Loopback106"
	//   [17] interface_type (Loopback=1): 01
	//   [18] interface_cyoa (None=0): 00
	//   [19] interface_dia (None=0): 00
	//   [20] loopback_type (Ipv4=2): 02
	//   [21-28] bandwidth: 0
	//   [29-36] cir: 0
	//   [37-38] mtu: 9000 (0x2328)
	//   [39] routing_mode (Static=0): 00
	//   [40-41] vlan_id: 0
	//   [42-46] ip_net: [cb, 00, 71, 28, 20] = 203.0.113.40/32
	//   [47-48] node_segment_idx: 0
	//   [49] user_tunnel_endpoint: 01 (true)

	// Use EXACT bytes from Rust serialization
	data := []byte{
		0x01,                   // [0] enum discriminant V2=1
		0x03,                   // [1] status Activated=3
		0x0b, 0x00, 0x00, 0x00, // [2-5] name length = 11
		0x4c, 0x6f, 0x6f, 0x70, 0x62, 0x61, 0x63, 0x6b, 0x31, 0x30, 0x36, // [6-16] "Loopback106"
		0x01,                                           // [17] interface_type Loopback=1
		0x00,                                           // [18] interface_cyoa None=0
		0x00,                                           // [19] interface_dia None=0
		0x02,                                           // [20] loopback_type Ipv4=2
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // [21-28] bandwidth=0
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // [29-36] cir=0
		0x28, 0x23, // [37-38] mtu=9000
		0x00,       // [39] routing_mode Static=0
		0x00, 0x00, // [40-41] vlan_id=0
		0xcb, 0x00, 0x71, 0x28, 0x20, // [42-46] ip_net 203.0.113.40/32
		0x00, 0x00, // [47-48] node_segment_idx=0
		0x01, // [49] user_tunnel_endpoint=true
	}

	t.Logf("Test data (%d bytes): %s", len(data), hex.EncodeToString(data))

	reader := NewByteReader(data)
	var iface Interface
	DeserializeInterface(reader, &iface)

	t.Logf("Deserialized interface:")
	t.Logf("  Version: %d", iface.Version)
	t.Logf("  Status: %d", iface.Status)
	t.Logf("  Name: %s", iface.Name)
	t.Logf("  InterfaceType: %d", iface.InterfaceType)
	t.Logf("  InterfaceCYOA: %d", iface.InterfaceCYOA)
	t.Logf("  InterfaceDIA: %d", iface.InterfaceDIA)
	t.Logf("  LoopbackType: %d", iface.LoopbackType)
	t.Logf("  Bandwidth: %d", iface.Bandwidth)
	t.Logf("  Cir: %d", iface.Cir)
	t.Logf("  Mtu: %d", iface.Mtu)
	t.Logf("  RoutingMode: %d", iface.RoutingMode)
	t.Logf("  VlanId: %d", iface.VlanId)
	t.Logf("  IpNet: %v", iface.IpNet)
	t.Logf("  NodeSegmentIdx: %d", iface.NodeSegmentIdx)
	t.Logf("  UserTunnelEndpoint: %v", iface.UserTunnelEndpoint)
	t.Logf("  Remaining bytes: %d", reader.Remaining())

	// Assertions
	if iface.Version != 1 {
		t.Errorf("Version: got %d, expected 1 (V2 enum discriminant)", iface.Version)
	}
	if iface.Status != InterfaceStatusActivated {
		t.Errorf("Status: got %d, expected %d (Activated)", iface.Status, InterfaceStatusActivated)
	}
	if iface.Name != "Loopback106" {
		t.Errorf("Name: got %s, expected Loopback106", iface.Name)
	}
	if iface.InterfaceType != InterfaceTypeLoopback {
		t.Errorf("InterfaceType: got %d, expected %d (Loopback)", iface.InterfaceType, InterfaceTypeLoopback)
	}
	if iface.LoopbackType != LoopbackTypeIpv4 {
		t.Errorf("LoopbackType: got %d, expected %d (Ipv4)", iface.LoopbackType, LoopbackTypeIpv4)
	}
	if iface.Mtu != 9000 {
		t.Errorf("Mtu: got %d, expected 9000", iface.Mtu)
	}
	if !iface.UserTunnelEndpoint {
		t.Errorf("UserTunnelEndpoint: got %v, expected true", iface.UserTunnelEndpoint)
	}
	if reader.Remaining() != 0 {
		t.Errorf("Should have consumed all bytes, but %d remaining", reader.Remaining())
	}
}
