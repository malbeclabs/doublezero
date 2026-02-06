package borshincremental

import (
	"encoding/binary"
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func leU16(v uint16) []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, v)
	return b
}

func leU32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

func leU64(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b
}

func leF64(v float64) []byte {
	return leU64(math.Float64bits(v))
}

// concat concatenates byte slices.
func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// ---------------------------------------------------------------------------
// 1. Happy path for every Read* method
// ---------------------------------------------------------------------------

func TestReadU8(t *testing.T) {
	r := NewReader([]byte{42})
	v, err := r.ReadU8()
	if err != nil {
		t.Fatal(err)
	}
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
	if r.Offset() != 1 {
		t.Fatalf("expected offset 1, got %d", r.Offset())
	}
	if r.Remaining() != 0 {
		t.Fatalf("expected 0 remaining, got %d", r.Remaining())
	}
}

func TestReadBool(t *testing.T) {
	r := NewReader([]byte{1, 0})
	v, err := r.ReadBool()
	if err != nil {
		t.Fatal(err)
	}
	if !v {
		t.Fatal("expected true")
	}
	v, err = r.ReadBool()
	if err != nil {
		t.Fatal(err)
	}
	if v {
		t.Fatal("expected false")
	}
	if r.Offset() != 2 {
		t.Fatalf("expected offset 2, got %d", r.Offset())
	}
}

func TestReadU16(t *testing.T) {
	r := NewReader(leU16(0xABCD))
	v, err := r.ReadU16()
	if err != nil {
		t.Fatal(err)
	}
	if v != 0xABCD {
		t.Fatalf("expected 0xABCD, got 0x%X", v)
	}
	if r.Offset() != 2 {
		t.Fatalf("expected offset 2, got %d", r.Offset())
	}
}

func TestReadU32(t *testing.T) {
	r := NewReader(leU32(123456))
	v, err := r.ReadU32()
	if err != nil {
		t.Fatal(err)
	}
	if v != 123456 {
		t.Fatalf("expected 123456, got %d", v)
	}
	if r.Offset() != 4 {
		t.Fatalf("expected offset 4, got %d", r.Offset())
	}
}

func TestReadU64(t *testing.T) {
	r := NewReader(leU64(0xDEADBEEFCAFEBABE))
	v, err := r.ReadU64()
	if err != nil {
		t.Fatal(err)
	}
	if v != 0xDEADBEEFCAFEBABE {
		t.Fatalf("expected 0xDEADBEEFCAFEBABE, got 0x%X", v)
	}
	if r.Offset() != 8 {
		t.Fatalf("expected offset 8, got %d", r.Offset())
	}
}

func TestReadU128(t *testing.T) {
	var buf [16]byte
	for i := range buf {
		buf[i] = byte(i)
	}
	r := NewReader(buf[:])
	v, err := r.ReadU128()
	if err != nil {
		t.Fatal(err)
	}
	if v != buf {
		t.Fatalf("unexpected u128: %x", v)
	}
	if r.Offset() != 16 {
		t.Fatalf("expected offset 16, got %d", r.Offset())
	}
}

func TestReadF64(t *testing.T) {
	r := NewReader(leF64(3.14))
	v, err := r.ReadF64()
	if err != nil {
		t.Fatal(err)
	}
	if v != 3.14 {
		t.Fatalf("expected 3.14, got %f", v)
	}
	if r.Offset() != 8 {
		t.Fatalf("expected offset 8, got %d", r.Offset())
	}
}

func TestReadPubkey(t *testing.T) {
	var buf [32]byte
	buf[0] = 1
	buf[31] = 0xff
	r := NewReader(buf[:])
	v, err := r.ReadPubkey()
	if err != nil {
		t.Fatal(err)
	}
	if v[0] != 1 || v[31] != 0xff {
		t.Fatalf("unexpected pubkey: %x", v)
	}
	if r.Offset() != 32 {
		t.Fatalf("expected offset 32, got %d", r.Offset())
	}
}

func TestReadIPv4(t *testing.T) {
	buf := [4]byte{192, 168, 1, 1}
	r := NewReader(buf[:])
	v, err := r.ReadIPv4()
	if err != nil {
		t.Fatal(err)
	}
	if v != buf {
		t.Fatalf("unexpected ipv4: %v", v)
	}
	if r.Offset() != 4 {
		t.Fatalf("expected offset 4, got %d", r.Offset())
	}
}

func TestReadNetworkV4(t *testing.T) {
	buf := [5]byte{10, 0, 0, 0, 24}
	r := NewReader(buf[:])
	v, err := r.ReadNetworkV4()
	if err != nil {
		t.Fatal(err)
	}
	if v != buf {
		t.Fatalf("unexpected: %v", v)
	}
	if r.Offset() != 5 {
		t.Fatalf("expected offset 5, got %d", r.Offset())
	}
}

func TestReadString(t *testing.T) {
	s := "hello"
	buf := concat(leU32(uint32(len(s))), []byte(s))
	r := NewReader(buf)
	v, err := r.ReadString()
	if err != nil {
		t.Fatal(err)
	}
	if v != "hello" {
		t.Fatalf("expected hello, got %s", v)
	}
	if r.Offset() != 9 {
		t.Fatalf("expected offset 9, got %d", r.Offset())
	}
}

func TestReadStringEmpty(t *testing.T) {
	r := NewReader(leU32(0))
	v, err := r.ReadString()
	if err != nil {
		t.Fatal(err)
	}
	if v != "" {
		t.Fatalf("expected empty string, got %q", v)
	}
	if r.Offset() != 4 {
		t.Fatalf("expected offset 4, got %d", r.Offset())
	}
}

func TestReadBytes(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	r := NewReader(data)
	v, err := r.ReadBytes(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 3 || v[0] != 1 || v[1] != 2 || v[2] != 3 {
		t.Fatalf("unexpected bytes: %v", v)
	}
	if r.Offset() != 3 {
		t.Fatalf("expected offset 3, got %d", r.Offset())
	}
}

func TestReadPubkeySlice(t *testing.T) {
	pk := [32]byte{1, 2, 3}
	buf := concat(leU32(1), pk[:])
	r := NewReader(buf)
	v, err := r.ReadPubkeySlice()
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 1 || v[0] != pk {
		t.Fatalf("unexpected: %v", v)
	}
}

func TestReadNetworkV4Slice(t *testing.T) {
	n1 := [5]byte{10, 0, 0, 0, 8}
	n2 := [5]byte{172, 16, 0, 0, 12}
	buf := concat(leU32(2), n1[:], n2[:])
	r := NewReader(buf)
	v, err := r.ReadNetworkV4Slice()
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 2 || v[0] != n1 || v[1] != n2 {
		t.Fatalf("unexpected: %v", v)
	}
}

func TestReadU32Slice(t *testing.T) {
	buf := concat(leU32(3), leU32(100), leU32(200), leU32(300))
	r := NewReader(buf)
	v, err := r.ReadU32Slice()
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 3 || v[0] != 100 || v[1] != 200 || v[2] != 300 {
		t.Fatalf("unexpected: %v", v)
	}
}

// ---------------------------------------------------------------------------
// 2. Error case: empty buffer for every Read* method
// ---------------------------------------------------------------------------

func TestReadU8Error(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadU8()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadBoolError(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadBool()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadU16Error(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadU16()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadU32Error(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadU32()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadU64Error(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadU64()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadU128Error(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadU128()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadF64Error(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadF64()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadPubkeyError(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadPubkey()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadIPv4Error(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadIPv4()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadNetworkV4Error(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadNetworkV4()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadStringError(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadString()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadBytesError(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadBytes(5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadPubkeySliceError(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadPubkeySlice()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadNetworkV4SliceError(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadNetworkV4Slice()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadU32SliceError(t *testing.T) {
	r := NewReader([]byte{})
	_, err := r.ReadU32Slice()
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// 3. Partial data error for multi-byte Read* methods
// ---------------------------------------------------------------------------

func TestReadU16Partial(t *testing.T) {
	r := NewReader([]byte{0x01}) // need 2, have 1
	_, err := r.ReadU16()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadU32Partial(t *testing.T) {
	r := NewReader([]byte{0x01, 0x02}) // need 4, have 2
	_, err := r.ReadU32()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadU64Partial(t *testing.T) {
	r := NewReader([]byte{1, 2, 3, 4}) // need 8, have 4
	_, err := r.ReadU64()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadU128Partial(t *testing.T) {
	r := NewReader(make([]byte, 10)) // need 16, have 10
	_, err := r.ReadU128()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadF64Partial(t *testing.T) {
	r := NewReader([]byte{1, 2, 3}) // need 8, have 3
	_, err := r.ReadF64()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadPubkeyPartial(t *testing.T) {
	r := NewReader(make([]byte, 20)) // need 32, have 20
	_, err := r.ReadPubkey()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadIPv4Partial(t *testing.T) {
	r := NewReader([]byte{10, 0}) // need 4, have 2
	_, err := r.ReadIPv4()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadNetworkV4Partial(t *testing.T) {
	r := NewReader([]byte{10, 0, 0}) // need 5, have 3
	_, err := r.ReadNetworkV4()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadBytesPartial(t *testing.T) {
	r := NewReader([]byte{1, 2}) // request 5, have 2
	_, err := r.ReadBytes(5)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// 4. TryRead* returns default on empty buffer
// ---------------------------------------------------------------------------

func TestTryReadU8Default(t *testing.T) {
	r := NewReader([]byte{})
	if v := r.TryReadU8(99); v != 99 {
		t.Fatalf("expected 99, got %d", v)
	}
}

func TestTryReadBoolDefault(t *testing.T) {
	r := NewReader([]byte{})
	if v := r.TryReadBool(true); !v {
		t.Fatal("expected true")
	}
}

func TestTryReadU16Default(t *testing.T) {
	r := NewReader([]byte{})
	if v := r.TryReadU16(9999); v != 9999 {
		t.Fatalf("expected 9999, got %d", v)
	}
}

func TestTryReadU32Default(t *testing.T) {
	r := NewReader([]byte{})
	if v := r.TryReadU32(777); v != 777 {
		t.Fatalf("expected 777, got %d", v)
	}
}

func TestTryReadU64Default(t *testing.T) {
	r := NewReader([]byte{})
	if v := r.TryReadU64(12345); v != 12345 {
		t.Fatalf("expected 12345, got %d", v)
	}
}

func TestTryReadU128Default(t *testing.T) {
	def := [16]byte{0xff}
	r := NewReader([]byte{})
	if v := r.TryReadU128(def); v != def {
		t.Fatalf("expected default, got %x", v)
	}
}

func TestTryReadF64Default(t *testing.T) {
	r := NewReader([]byte{})
	if v := r.TryReadF64(1.5); v != 1.5 {
		t.Fatalf("expected 1.5, got %f", v)
	}
}

func TestTryReadPubkeyDefault(t *testing.T) {
	def := [32]byte{0xAA}
	r := NewReader([]byte{})
	if v := r.TryReadPubkey(def); v != def {
		t.Fatalf("expected default, got %x", v)
	}
}

func TestTryReadIPv4Default(t *testing.T) {
	def := [4]byte{127, 0, 0, 1}
	r := NewReader([]byte{})
	if v := r.TryReadIPv4(def); v != def {
		t.Fatalf("expected default, got %v", v)
	}
}

func TestTryReadNetworkV4Default(t *testing.T) {
	def := [5]byte{10, 0, 0, 0, 8}
	r := NewReader([]byte{})
	if v := r.TryReadNetworkV4(def); v != def {
		t.Fatalf("expected default, got %v", v)
	}
}

func TestTryReadStringDefault(t *testing.T) {
	r := NewReader([]byte{})
	if v := r.TryReadString("fallback"); v != "fallback" {
		t.Fatalf("expected fallback, got %s", v)
	}
}

func TestTryReadPubkeySliceDefault(t *testing.T) {
	r := NewReader([]byte{})
	if v := r.TryReadPubkeySlice(nil); v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

func TestTryReadNetworkV4SliceDefault(t *testing.T) {
	r := NewReader([]byte{})
	if v := r.TryReadNetworkV4Slice(nil); v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

func TestTryReadU32SliceDefault(t *testing.T) {
	r := NewReader([]byte{})
	if v := r.TryReadU32Slice(nil); v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

// ---------------------------------------------------------------------------
// 5. TryRead* returns actual value when data exists
// ---------------------------------------------------------------------------

func TestTryReadU8Value(t *testing.T) {
	r := NewReader([]byte{42})
	if v := r.TryReadU8(99); v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
}

func TestTryReadBoolValue(t *testing.T) {
	r := NewReader([]byte{1})
	if v := r.TryReadBool(false); !v {
		t.Fatal("expected true")
	}
}

func TestTryReadU16Value(t *testing.T) {
	r := NewReader(leU16(500))
	if v := r.TryReadU16(0); v != 500 {
		t.Fatalf("expected 500, got %d", v)
	}
}

func TestTryReadU32Value(t *testing.T) {
	r := NewReader(leU32(100000))
	if v := r.TryReadU32(0); v != 100000 {
		t.Fatalf("expected 100000, got %d", v)
	}
}

func TestTryReadU64Value(t *testing.T) {
	r := NewReader(leU64(9999999999))
	if v := r.TryReadU64(0); v != 9999999999 {
		t.Fatalf("expected 9999999999, got %d", v)
	}
}

func TestTryReadU128Value(t *testing.T) {
	var data [16]byte
	data[0] = 0x42
	r := NewReader(data[:])
	if v := r.TryReadU128([16]byte{}); v != data {
		t.Fatalf("expected %x, got %x", data, v)
	}
}

func TestTryReadF64Value(t *testing.T) {
	r := NewReader(leF64(2.718))
	if v := r.TryReadF64(0); v != 2.718 {
		t.Fatalf("expected 2.718, got %f", v)
	}
}

func TestTryReadPubkeyValue(t *testing.T) {
	var pk [32]byte
	pk[0] = 0xBB
	r := NewReader(pk[:])
	if v := r.TryReadPubkey([32]byte{}); v != pk {
		t.Fatalf("expected %x, got %x", pk, v)
	}
}

func TestTryReadIPv4Value(t *testing.T) {
	data := [4]byte{10, 20, 30, 40}
	r := NewReader(data[:])
	if v := r.TryReadIPv4([4]byte{}); v != data {
		t.Fatalf("expected %v, got %v", data, v)
	}
}

func TestTryReadNetworkV4Value(t *testing.T) {
	data := [5]byte{172, 16, 0, 0, 16}
	r := NewReader(data[:])
	if v := r.TryReadNetworkV4([5]byte{}); v != data {
		t.Fatalf("expected %v, got %v", data, v)
	}
}

func TestTryReadStringValue(t *testing.T) {
	s := "world"
	buf := concat(leU32(uint32(len(s))), []byte(s))
	r := NewReader(buf)
	if v := r.TryReadString("def"); v != "world" {
		t.Fatalf("expected world, got %s", v)
	}
}

func TestTryReadPubkeySliceValue(t *testing.T) {
	pk := [32]byte{0xAA}
	buf := concat(leU32(1), pk[:])
	r := NewReader(buf)
	v := r.TryReadPubkeySlice(nil)
	if len(v) != 1 || v[0] != pk {
		t.Fatalf("unexpected: %v", v)
	}
}

func TestTryReadNetworkV4SliceValue(t *testing.T) {
	n := [5]byte{10, 0, 0, 0, 24}
	buf := concat(leU32(1), n[:])
	r := NewReader(buf)
	v := r.TryReadNetworkV4Slice(nil)
	if len(v) != 1 || v[0] != n {
		t.Fatalf("unexpected: %v", v)
	}
}

func TestTryReadU32SliceValue(t *testing.T) {
	buf := concat(leU32(2), leU32(11), leU32(22))
	r := NewReader(buf)
	v := r.TryReadU32Slice(nil)
	if len(v) != 2 || v[0] != 11 || v[1] != 22 {
		t.Fatalf("unexpected: %v", v)
	}
}

// ---------------------------------------------------------------------------
// 6. TryRead* with partial data returns default
// ---------------------------------------------------------------------------

func TestTryReadU16Partial(t *testing.T) {
	r := NewReader([]byte{0x01}) // need 2, have 1
	if v := r.TryReadU16(5555); v != 5555 {
		t.Fatalf("expected 5555, got %d", v)
	}
}

func TestTryReadU32Partial(t *testing.T) {
	r := NewReader([]byte{1, 2, 3}) // need 4, have 3
	if v := r.TryReadU32(777); v != 777 {
		t.Fatalf("expected 777, got %d", v)
	}
}

func TestTryReadU64Partial(t *testing.T) {
	r := NewReader(make([]byte, 5)) // need 8, have 5
	if v := r.TryReadU64(42); v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
}

func TestTryReadU128Partial(t *testing.T) {
	def := [16]byte{0xFF}
	r := NewReader(make([]byte, 10)) // need 16, have 10
	if v := r.TryReadU128(def); v != def {
		t.Fatalf("expected default, got %x", v)
	}
}

func TestTryReadF64Partial(t *testing.T) {
	r := NewReader(make([]byte, 4)) // need 8, have 4
	if v := r.TryReadF64(1.23); v != 1.23 {
		t.Fatalf("expected 1.23, got %f", v)
	}
}

func TestTryReadPubkeyPartial(t *testing.T) {
	def := [32]byte{0xDD}
	r := NewReader(make([]byte, 20)) // need 32, have 20
	if v := r.TryReadPubkey(def); v != def {
		t.Fatalf("expected default, got %x", v)
	}
}

func TestTryReadIPv4Partial(t *testing.T) {
	def := [4]byte{127, 0, 0, 1}
	r := NewReader([]byte{10, 20}) // need 4, have 2
	if v := r.TryReadIPv4(def); v != def {
		t.Fatalf("expected default, got %v", v)
	}
}

func TestTryReadNetworkV4Partial(t *testing.T) {
	def := [5]byte{1, 2, 3, 4, 5}
	r := NewReader([]byte{10, 0, 0}) // need 5, have 3
	if v := r.TryReadNetworkV4(def); v != def {
		t.Fatalf("expected default, got %v", v)
	}
}

func TestTryReadStringPartialLength(t *testing.T) {
	r := NewReader([]byte{0x05, 0x00}) // need 4 for length prefix, have 2
	if v := r.TryReadString("def"); v != "def" {
		t.Fatalf("expected def, got %s", v)
	}
}

func TestTryReadPubkeySlicePartial(t *testing.T) {
	r := NewReader([]byte{1, 2}) // need 4 for length prefix, have 2
	if v := r.TryReadPubkeySlice(nil); v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

func TestTryReadNetworkV4SlicePartial(t *testing.T) {
	r := NewReader([]byte{1}) // need 4 for length prefix, have 1
	if v := r.TryReadNetworkV4Slice(nil); v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

func TestTryReadU32SlicePartial(t *testing.T) {
	r := NewReader([]byte{1, 2, 3}) // need 4 for length prefix, have 3
	if v := r.TryReadU32Slice(nil); v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

// ---------------------------------------------------------------------------
// 7. Sequential reads
// ---------------------------------------------------------------------------

func TestSequentialReads(t *testing.T) {
	// u8(1) + u16(1000) + u32(50000) + u64(big) + bool(true)
	buf := concat(
		[]byte{0x01},
		leU16(1000),
		leU32(50000),
		leU64(0x0102030405060708),
		[]byte{0x01},
	)
	r := NewReader(buf)

	v1, err := r.ReadU8()
	if err != nil {
		t.Fatal(err)
	}
	if v1 != 1 {
		t.Fatalf("expected 1, got %d", v1)
	}
	if r.Offset() != 1 {
		t.Fatalf("expected offset 1, got %d", r.Offset())
	}

	v2, err := r.ReadU16()
	if err != nil {
		t.Fatal(err)
	}
	if v2 != 1000 {
		t.Fatalf("expected 1000, got %d", v2)
	}
	if r.Offset() != 3 {
		t.Fatalf("expected offset 3, got %d", r.Offset())
	}

	v3, err := r.ReadU32()
	if err != nil {
		t.Fatal(err)
	}
	if v3 != 50000 {
		t.Fatalf("expected 50000, got %d", v3)
	}
	if r.Offset() != 7 {
		t.Fatalf("expected offset 7, got %d", r.Offset())
	}

	v4, err := r.ReadU64()
	if err != nil {
		t.Fatal(err)
	}
	if v4 != 0x0102030405060708 {
		t.Fatalf("expected 0x0102030405060708, got 0x%X", v4)
	}
	if r.Offset() != 15 {
		t.Fatalf("expected offset 15, got %d", r.Offset())
	}

	v5, err := r.ReadBool()
	if err != nil {
		t.Fatal(err)
	}
	if !v5 {
		t.Fatal("expected true")
	}
	if r.Offset() != 16 {
		t.Fatalf("expected offset 16, got %d", r.Offset())
	}

	if r.Remaining() != 0 {
		t.Fatalf("expected 0 remaining, got %d", r.Remaining())
	}
}

// ---------------------------------------------------------------------------
// 8. Trailing fields scenario
// ---------------------------------------------------------------------------

func TestTrailingFieldsDefault(t *testing.T) {
	// Struct: u8, u32 required; then optional trailing u16, string, pubkey.
	buf := make([]byte, 5)
	buf[0] = 1
	binary.LittleEndian.PutUint32(buf[1:], 42)

	r := NewReader(buf)
	v1, err := r.ReadU8()
	if err != nil {
		t.Fatal(err)
	}
	if v1 != 1 {
		t.Fatalf("expected 1, got %d", v1)
	}

	v2, err := r.ReadU32()
	if err != nil {
		t.Fatal(err)
	}
	if v2 != 42 {
		t.Fatalf("expected 42, got %d", v2)
	}

	// All trailing optional fields missing.
	if v := r.TryReadU16(9999); v != 9999 {
		t.Fatalf("expected 9999, got %d", v)
	}
	if v := r.TryReadString("none"); v != "none" {
		t.Fatalf("expected none, got %s", v)
	}
	def := [32]byte{}
	if v := r.TryReadPubkey(def); v != def {
		t.Fatalf("expected default pubkey, got %x", v)
	}
}

func TestTrailingFieldsSomePresent(t *testing.T) {
	// Struct: u8, u32 required; then optional trailing u16 present, string missing.
	buf := concat([]byte{1}, leU32(42), leU16(7777))

	r := NewReader(buf)
	_, _ = r.ReadU8()
	_, _ = r.ReadU32()

	if v := r.TryReadU16(0); v != 7777 {
		t.Fatalf("expected 7777, got %d", v)
	}
	// Next trailing field missing.
	if v := r.TryReadString("missing"); v != "missing" {
		t.Fatalf("expected missing, got %s", v)
	}
}

// ---------------------------------------------------------------------------
// 9. Vec/Slice methods: empty, single, multiple, truncated
// ---------------------------------------------------------------------------

func TestReadPubkeySliceEmpty(t *testing.T) {
	r := NewReader(leU32(0))
	v, err := r.ReadPubkeySlice()
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Fatalf("expected nil for empty vec, got %v", v)
	}
}

func TestReadPubkeySliceMultiple(t *testing.T) {
	pk1 := [32]byte{1}
	pk2 := [32]byte{2}
	pk3 := [32]byte{3}
	buf := concat(leU32(3), pk1[:], pk2[:], pk3[:])
	r := NewReader(buf)
	v, err := r.ReadPubkeySlice()
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 3 || v[0] != pk1 || v[1] != pk2 || v[2] != pk3 {
		t.Fatalf("unexpected: %v", v)
	}
}

func TestReadPubkeySliceTruncatedLength(t *testing.T) {
	r := NewReader([]byte{0x01, 0x00}) // incomplete u32 length prefix
	_, err := r.ReadPubkeySlice()
	if err == nil {
		t.Fatal("expected error for truncated length")
	}
}

func TestReadPubkeySliceTruncatedElements(t *testing.T) {
	// length=2 but only 1 pubkey worth of data
	buf := concat(leU32(2), make([]byte, 32))
	r := NewReader(buf)
	_, err := r.ReadPubkeySlice()
	if err == nil {
		t.Fatal("expected error for truncated elements")
	}
}

func TestReadNetworkV4SliceEmpty(t *testing.T) {
	r := NewReader(leU32(0))
	v, err := r.ReadNetworkV4Slice()
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

func TestReadNetworkV4SliceTruncatedElements(t *testing.T) {
	// length=3 but only 2 elements worth of data
	buf := concat(leU32(3), make([]byte, 10))
	r := NewReader(buf)
	_, err := r.ReadNetworkV4Slice()
	if err == nil {
		t.Fatal("expected error for truncated elements")
	}
}

func TestReadU32SliceEmpty(t *testing.T) {
	r := NewReader(leU32(0))
	v, err := r.ReadU32Slice()
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

func TestReadU32SliceTruncatedLength(t *testing.T) {
	r := NewReader([]byte{0x02, 0x00}) // incomplete u32 length prefix
	_, err := r.ReadU32Slice()
	if err == nil {
		t.Fatal("expected error for truncated length")
	}
}

func TestReadU32SliceTruncatedElements(t *testing.T) {
	// length=3 but only 2 elements worth of data
	buf := concat(leU32(3), leU32(1), leU32(2))
	r := NewReader(buf)
	_, err := r.ReadU32Slice()
	if err == nil {
		t.Fatal("expected error for truncated elements")
	}
}

// ---------------------------------------------------------------------------
// 10. String edge cases
// ---------------------------------------------------------------------------

func TestReadStringTruncated(t *testing.T) {
	// length says 10 but only 5 bytes of content
	buf := concat(leU32(10), []byte("hello"))
	r := NewReader(buf)
	_, err := r.ReadString()
	if err == nil {
		t.Fatal("expected error for truncated string")
	}
}

func TestReadStringTruncatedAdvancesOffset(t *testing.T) {
	// After a truncated string, offset should have advanced past the length prefix.
	buf := concat(leU32(10), []byte("hi"))
	r := NewReader(buf)
	_, err := r.ReadString()
	if err == nil {
		t.Fatal("expected error")
	}
	// ReadU32 for length succeeded, so offset moved by 4.
	if r.Offset() != 4 {
		t.Fatalf("expected offset 4, got %d", r.Offset())
	}
}

// ---------------------------------------------------------------------------
// 11. U128 byte order (little-endian)
// ---------------------------------------------------------------------------

func TestReadU128ByteOrder(t *testing.T) {
	// Store value 1 as u128 little-endian: byte[0]=1, rest=0.
	var buf [16]byte
	buf[0] = 1
	r := NewReader(buf[:])
	v, err := r.ReadU128()
	if err != nil {
		t.Fatal(err)
	}
	if v[0] != 1 {
		t.Fatalf("expected LSB=1, got %d", v[0])
	}
	for i := 1; i < 16; i++ {
		if v[i] != 0 {
			t.Fatalf("expected byte[%d]=0, got %d", i, v[i])
		}
	}

	// Store 0x0102...10 and verify byte order is preserved.
	for i := range buf {
		buf[i] = byte(i + 1)
	}
	r = NewReader(buf[:])
	v, err = r.ReadU128()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 16 {
		if v[i] != byte(i+1) {
			t.Fatalf("byte[%d]: expected %d, got %d", i, i+1, v[i])
		}
	}
}

// ---------------------------------------------------------------------------
// 12. F64 known float value
// ---------------------------------------------------------------------------

func TestReadF64KnownValue(t *testing.T) {
	// math.Pi
	r := NewReader(leF64(math.Pi))
	v, err := r.ReadF64()
	if err != nil {
		t.Fatal(err)
	}
	if v != math.Pi {
		t.Fatalf("expected Pi (%v), got %v", math.Pi, v)
	}
}

func TestReadF64NegativeZero(t *testing.T) {
	nz := math.Copysign(0, -1)
	r := NewReader(leF64(nz))
	v, err := r.ReadF64()
	if err != nil {
		t.Fatal(err)
	}
	if math.Float64bits(v) != math.Float64bits(nz) {
		t.Fatalf("expected negative zero, got %v", v)
	}
}

func TestReadF64Inf(t *testing.T) {
	r := NewReader(leF64(math.Inf(1)))
	v, err := r.ReadF64()
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsInf(v, 1) {
		t.Fatalf("expected +Inf, got %v", v)
	}
}

// ---------------------------------------------------------------------------
// 13. Offset() and Remaining() correctness
// ---------------------------------------------------------------------------

func TestOffsetAndRemaining(t *testing.T) {
	buf := make([]byte, 20)
	r := NewReader(buf)

	if r.Offset() != 0 {
		t.Fatalf("expected offset 0, got %d", r.Offset())
	}
	if r.Remaining() != 20 {
		t.Fatalf("expected 20 remaining, got %d", r.Remaining())
	}

	_, _ = r.ReadU8()
	if r.Offset() != 1 || r.Remaining() != 19 {
		t.Fatalf("after ReadU8: offset=%d remaining=%d", r.Offset(), r.Remaining())
	}

	_, _ = r.ReadU32()
	if r.Offset() != 5 || r.Remaining() != 15 {
		t.Fatalf("after ReadU32: offset=%d remaining=%d", r.Offset(), r.Remaining())
	}

	_, _ = r.ReadU64()
	if r.Offset() != 13 || r.Remaining() != 7 {
		t.Fatalf("after ReadU64: offset=%d remaining=%d", r.Offset(), r.Remaining())
	}

	_, _ = r.ReadBytes(5)
	if r.Offset() != 18 || r.Remaining() != 2 {
		t.Fatalf("after ReadBytes: offset=%d remaining=%d", r.Offset(), r.Remaining())
	}

	_, _ = r.ReadU16()
	if r.Offset() != 20 || r.Remaining() != 0 {
		t.Fatalf("after ReadU16: offset=%d remaining=%d", r.Offset(), r.Remaining())
	}
}

func TestOffsetUnchangedOnError(t *testing.T) {
	r := NewReader([]byte{0x01})
	_, err := r.ReadU32() // fails, needs 4 bytes
	if err == nil {
		t.Fatal("expected error")
	}
	if r.Offset() != 0 {
		t.Fatalf("expected offset unchanged at 0, got %d", r.Offset())
	}
	if r.Remaining() != 1 {
		t.Fatalf("expected 1 remaining, got %d", r.Remaining())
	}
}

func TestTryReadDoesNotAdvanceOnDefault(t *testing.T) {
	r := NewReader([]byte{0x01})
	// TryReadU32 needs 4 bytes, only 1 available, returns default.
	v := r.TryReadU32(999)
	if v != 999 {
		t.Fatalf("expected 999, got %d", v)
	}
	if r.Offset() != 0 {
		t.Fatalf("expected offset 0, got %d", r.Offset())
	}
	// The byte is still available for a subsequent ReadU8.
	v2, err := r.ReadU8()
	if err != nil {
		t.Fatal(err)
	}
	if v2 != 1 {
		t.Fatalf("expected 1, got %d", v2)
	}
}
