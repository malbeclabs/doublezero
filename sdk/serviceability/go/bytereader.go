package serviceability

import (
	"encoding/binary"

	borsh "github.com/malbeclabs/doublezero/sdk/borsh-incremental/go"
)

// ByteReader wraps borshincremental.Reader with the legacy no-error API.
// All read methods use TryRead* with zero defaults to preserve backward
// compatibility (silently returning zero values on short data).
type ByteReader struct {
	r *borsh.Reader
}

func NewByteReader(data []byte) *ByteReader {
	return &ByteReader{r: borsh.NewReader(data)}
}

func (br *ByteReader) GetOffset() int    { return br.r.Offset() }
func (br *ByteReader) Remaining() uint32 { return uint32(br.r.Remaining()) }

func (br *ByteReader) ReadU8() uint8          { return br.r.TryReadU8(0) }
func (br *ByteReader) ReadBool() bool         { return br.r.TryReadBool(false) }
func (br *ByteReader) ReadU16() uint16        { return br.r.TryReadU16(0) }
func (br *ByteReader) ReadU32() uint32        { return br.r.TryReadU32(0) }
func (br *ByteReader) ReadU64() uint64        { return br.r.TryReadU64(0) }
func (br *ByteReader) ReadF64() float64       { return br.r.TryReadF64(0) }
func (br *ByteReader) ReadPubkey() [32]byte   { return br.r.TryReadPubkey([32]byte{}) }
func (br *ByteReader) ReadIPv4() [4]byte      { return br.r.TryReadIPv4([4]byte{}) }
func (br *ByteReader) ReadNetworkV4() [5]byte { return br.r.TryReadNetworkV4([5]byte{}) }

func (br *ByteReader) ReadU128() Uint128 {
	raw := br.r.TryReadU128([16]byte{})
	return Uint128{
		Low:  binary.LittleEndian.Uint64(raw[:8]),
		High: binary.LittleEndian.Uint64(raw[8:]),
	}
}

func (br *ByteReader) ReadString() string {
	return br.r.TryReadString("")
}

func (br *ByteReader) ReadPubkeySlice() [][32]byte {
	return br.r.TryReadPubkeySlice(nil)
}

func (br *ByteReader) ReadNetworkV4Slice() [][5]byte {
	return br.r.TryReadNetworkV4Slice(nil)
}

func (br *ByteReader) ReadU32Slice() []uint32 {
	return br.r.TryReadU32Slice(nil)
}

func (br *ByteReader) ReadBytes(n int) []byte {
	if br.r.Remaining() < n {
		return make([]byte, n)
	}
	v, _ := br.r.ReadBytes(n)
	return v
}

func (br *ByteReader) DumpBytes(n int) string {
	// Preserved for compatibility but uses offset from underlying reader.
	return ""
}
