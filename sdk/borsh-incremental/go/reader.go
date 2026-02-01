// Package borshincremental provides cursor-based reading of Borsh-serialized
// data with backward-compatible incremental deserialization.
//
// The key invariant: if the reader offset hasn't advanced when a read fails,
// the data is simply missing (trailing field) and the TryRead* methods return
// a default. If the offset advanced but the read still failed, the data is
// corrupt and the Read* methods return an error.
package borshincremental

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Reader provides cursor-based reading of Borsh-serialized binary data.
type Reader struct {
	data   []byte
	offset int
}

// NewReader creates a new Reader over the given byte slice.
func NewReader(data []byte) *Reader {
	return &Reader{data: data, offset: 0}
}

// Offset returns the current read position.
func (r *Reader) Offset() int {
	return r.offset
}

// Remaining returns the number of unread bytes.
func (r *Reader) Remaining() int {
	return len(r.data) - r.offset
}

// --- Strict read methods (error on insufficient data) ---

func (r *Reader) ReadU8() (uint8, error) {
	if r.offset+1 > len(r.data) {
		return 0, fmt.Errorf("borsh: not enough data for u8 at offset %d", r.offset)
	}
	val := r.data[r.offset]
	r.offset++
	return val, nil
}

func (r *Reader) ReadBool() (bool, error) {
	v, err := r.ReadU8()
	return v != 0, err
}

func (r *Reader) ReadU16() (uint16, error) {
	if r.offset+2 > len(r.data) {
		return 0, fmt.Errorf("borsh: not enough data for u16 at offset %d", r.offset)
	}
	val := binary.LittleEndian.Uint16(r.data[r.offset:])
	r.offset += 2
	return val, nil
}

func (r *Reader) ReadU32() (uint32, error) {
	if r.offset+4 > len(r.data) {
		return 0, fmt.Errorf("borsh: not enough data for u32 at offset %d", r.offset)
	}
	val := binary.LittleEndian.Uint32(r.data[r.offset:])
	r.offset += 4
	return val, nil
}

func (r *Reader) ReadU64() (uint64, error) {
	if r.offset+8 > len(r.data) {
		return 0, fmt.Errorf("borsh: not enough data for u64 at offset %d", r.offset)
	}
	val := binary.LittleEndian.Uint64(r.data[r.offset:])
	r.offset += 8
	return val, nil
}

func (r *Reader) ReadU128() ([16]byte, error) {
	if r.offset+16 > len(r.data) {
		return [16]byte{}, fmt.Errorf("borsh: not enough data for u128 at offset %d", r.offset)
	}
	var val [16]byte
	copy(val[:], r.data[r.offset:r.offset+16])
	r.offset += 16
	return val, nil
}

func (r *Reader) ReadF64() (float64, error) {
	v, err := r.ReadU64()
	return math.Float64frombits(v), err
}

func (r *Reader) ReadPubkey() ([32]byte, error) {
	if r.offset+32 > len(r.data) {
		return [32]byte{}, fmt.Errorf("borsh: not enough data for pubkey at offset %d", r.offset)
	}
	val := [32]byte(r.data[r.offset : r.offset+32])
	r.offset += 32
	return val, nil
}

func (r *Reader) ReadIPv4() ([4]byte, error) {
	if r.offset+4 > len(r.data) {
		return [4]byte{}, fmt.Errorf("borsh: not enough data for ipv4 at offset %d", r.offset)
	}
	val := [4]byte(r.data[r.offset : r.offset+4])
	r.offset += 4
	return val, nil
}

func (r *Reader) ReadNetworkV4() ([5]byte, error) {
	if r.offset+5 > len(r.data) {
		return [5]byte{}, fmt.Errorf("borsh: not enough data for network_v4 at offset %d", r.offset)
	}
	val := [5]byte(r.data[r.offset : r.offset+5])
	r.offset += 5
	return val, nil
}

func (r *Reader) ReadString() (string, error) {
	length, err := r.ReadU32()
	if err != nil {
		return "", err
	}
	if length == 0 {
		return "", nil
	}
	if r.offset+int(length) > len(r.data) {
		return "", fmt.Errorf("borsh: not enough data for string of length %d at offset %d", length, r.offset)
	}
	val := string(r.data[r.offset : r.offset+int(length)])
	r.offset += int(length)
	return val, nil
}

func (r *Reader) ReadBytes(n int) ([]byte, error) {
	if r.offset+n > len(r.data) {
		return nil, fmt.Errorf("borsh: not enough data for %d bytes at offset %d", n, r.offset)
	}
	val := make([]byte, n)
	copy(val, r.data[r.offset:r.offset+n])
	r.offset += n
	return val, nil
}

func (r *Reader) ReadPubkeySlice() ([][32]byte, error) {
	length, err := r.ReadU32()
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return nil, nil
	}
	if int(length)*32 > r.Remaining() {
		return nil, fmt.Errorf("borsh: not enough data for %d pubkeys at offset %d", length, r.offset)
	}
	result := make([][32]byte, length)
	for i := range int(length) {
		result[i], err = r.ReadPubkey()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *Reader) ReadNetworkV4Slice() ([][5]byte, error) {
	length, err := r.ReadU32()
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return nil, nil
	}
	if int(length)*5 > r.Remaining() {
		return nil, fmt.Errorf("borsh: not enough data for %d network_v4 at offset %d", length, r.offset)
	}
	result := make([][5]byte, length)
	for i := range int(length) {
		result[i], err = r.ReadNetworkV4()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *Reader) ReadU32Slice() ([]uint32, error) {
	length, err := r.ReadU32()
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return nil, nil
	}
	if int(length)*4 > r.Remaining() {
		return nil, fmt.Errorf("borsh: not enough data for %d u32s at offset %d", length, r.offset)
	}
	result := make([]uint32, length)
	for i := range int(length) {
		result[i], err = r.ReadU32()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// --- Try variants (return default when no bytes available at field boundary) ---

func (r *Reader) TryReadU8(def uint8) uint8 {
	if r.Remaining() < 1 {
		return def
	}
	v, _ := r.ReadU8()
	return v
}

func (r *Reader) TryReadBool(def bool) bool {
	if r.Remaining() < 1 {
		return def
	}
	v, _ := r.ReadBool()
	return v
}

func (r *Reader) TryReadU16(def uint16) uint16 {
	if r.Remaining() < 2 {
		return def
	}
	v, _ := r.ReadU16()
	return v
}

func (r *Reader) TryReadU32(def uint32) uint32 {
	if r.Remaining() < 4 {
		return def
	}
	v, _ := r.ReadU32()
	return v
}

func (r *Reader) TryReadU64(def uint64) uint64 {
	if r.Remaining() < 8 {
		return def
	}
	v, _ := r.ReadU64()
	return v
}

func (r *Reader) TryReadU128(def [16]byte) [16]byte {
	if r.Remaining() < 16 {
		return def
	}
	v, _ := r.ReadU128()
	return v
}

func (r *Reader) TryReadF64(def float64) float64 {
	if r.Remaining() < 8 {
		return def
	}
	v, _ := r.ReadF64()
	return v
}

func (r *Reader) TryReadPubkey(def [32]byte) [32]byte {
	if r.Remaining() < 32 {
		return def
	}
	v, _ := r.ReadPubkey()
	return v
}

func (r *Reader) TryReadIPv4(def [4]byte) [4]byte {
	if r.Remaining() < 4 {
		return def
	}
	v, _ := r.ReadIPv4()
	return v
}

func (r *Reader) TryReadNetworkV4(def [5]byte) [5]byte {
	if r.Remaining() < 5 {
		return def
	}
	v, _ := r.ReadNetworkV4()
	return v
}

func (r *Reader) TryReadString(def string) string {
	if r.Remaining() < 4 {
		return def
	}
	v, _ := r.ReadString()
	return v
}

func (r *Reader) TryReadPubkeySlice(def [][32]byte) [][32]byte {
	if r.Remaining() < 4 {
		return def
	}
	v, _ := r.ReadPubkeySlice()
	return v
}

func (r *Reader) TryReadNetworkV4Slice(def [][5]byte) [][5]byte {
	if r.Remaining() < 4 {
		return def
	}
	v, _ := r.ReadNetworkV4Slice()
	return v
}

func (r *Reader) TryReadU32Slice(def []uint32) []uint32 {
	if r.Remaining() < 4 {
		return def
	}
	v, _ := r.ReadU32Slice()
	return v
}
