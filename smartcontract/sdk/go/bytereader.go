package dzsdk

import (
	"encoding/binary"
	"math"
)

type ByteReader struct {
	data   []byte
	offset int
}

func NewByteReader(data []byte) *ByteReader {
	return &ByteReader{data: data, offset: 0}
}

func (br *ByteReader) ReadU8() uint8 {
	if br.offset+1 > len(br.data) {
		return 0
	}
	val := br.data[br.offset]
	br.offset++
	return val
}

func (br *ByteReader) ReadU16() uint16 {
	if br.offset+2 > len(br.data) {
		return 0
	}
	val := binary.LittleEndian.Uint16(br.data[br.offset:])
	br.offset += 2
	return val
}

func (br *ByteReader) ReadU32() uint32 {
	if br.offset+4 > len(br.data) {
		return 0
	}
	val := binary.LittleEndian.Uint32(br.data[br.offset:])
	br.offset += 4
	return val
}

func (br *ByteReader) ReadU64() uint64 {
	if br.offset+8 > len(br.data) {
		return 0
	}
	val := binary.LittleEndian.Uint64(br.data[br.offset:])
	br.offset += 8
	return val
}

func (br *ByteReader) ReadU128() Uint128 {
	if br.offset+16 > len(br.data) {
		return Uint128{0, 0}
	}
	var val Uint128
	val.High = binary.LittleEndian.Uint64(br.data[br.offset : br.offset+8])
	val.Low = binary.LittleEndian.Uint64(br.data[br.offset+8 : br.offset+16])
	br.offset += 16
	return val
}

func (br *ByteReader) ReadF64() float64 {
	return math.Float64frombits(br.ReadU64())
}

func (br *ByteReader) ReadPubkey() [32]byte {
	if br.offset+32 > len(br.data) {
		return [32]byte{}
	}
	val := [32]byte(br.data[br.offset : br.offset+32])
	br.offset += 32
	return val
}

func (br *ByteReader) ReadPubkeySlice() [][32]byte {
	length := br.ReadU32()
	if length == 0 {
		return nil
	}
	result := make([][32]byte, length)
	for i := uint32(0); i < length; i++ {
		result[i] = br.ReadPubkey()
	}
	return result
}

func (br *ByteReader) ReadIPv4() [4]byte {
	if br.offset+4 > len(br.data) {
		return [4]byte{}
	}
	val := [4]byte(br.data[br.offset : br.offset+4])
	br.offset += 4
	return val
}

func (br *ByteReader) ReadNetworkV4() [5]byte {
	if br.offset+5 > len(br.data) {
		return [5]byte{}
	}
	val := [5]byte(br.data[br.offset : br.offset+5])
	br.offset += 5
	return val
}

func (br *ByteReader) ReadNetworkV4Slice() [][5]byte {
	length := br.ReadU32()
	if length == 0 {
		return nil
	}
	result := make([][5]byte, length)
	for i := uint32(0); i < length; i++ {
		result[i] = br.ReadNetworkV4()
	}
	return result
}

func (br *ByteReader) ReadString() string {
	length := br.ReadU32()
	if length == 0 {
		return ""
	}
	if br.offset+int(length) > len(br.data) {
		return ""
	}
	val := string(br.data[br.offset : br.offset+int(length)])
	br.offset += int(length)
	return val
}
