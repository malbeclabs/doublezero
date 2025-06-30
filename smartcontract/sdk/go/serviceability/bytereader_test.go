package serviceability

import (
	"encoding/binary"
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
		[32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},          //nolint
		[32]byte{33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64}, //nolint
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
		[5]byte{1, 2, 3, 4, 5},  // nolint
		[5]byte{6, 7, 8, 9, 10}, // nolint
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
