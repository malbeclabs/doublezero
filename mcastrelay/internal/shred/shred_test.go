package shred

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecode_LegacyDataShred(t *testing.T) {
	// Create a mock legacy data shred
	data := make([]byte, 200)

	// Signature (64 bytes) - just zeros for test
	// Variant = 0xA5 (Legacy Data)
	data[VariantOffset] = 0xA5

	// Slot = 12345678 (little endian)
	data[SlotOffset] = 0x4E
	data[SlotOffset+1] = 0x61
	data[SlotOffset+2] = 0xBC
	data[SlotOffset+3] = 0x00
	data[SlotOffset+4] = 0x00
	data[SlotOffset+5] = 0x00
	data[SlotOffset+6] = 0x00
	data[SlotOffset+7] = 0x00

	// Shred index = 42
	data[ShredIndexOffset] = 42
	data[ShredIndexOffset+1] = 0
	data[ShredIndexOffset+2] = 0
	data[ShredIndexOffset+3] = 0

	// Shred version = 1
	data[VersionOffset] = 1
	data[VersionOffset+1] = 0

	// FEC set index = 40
	data[FECSetIndexOffset] = 40
	data[FECSetIndexOffset+1] = 0
	data[FECSetIndexOffset+2] = 0
	data[FECSetIndexOffset+3] = 0

	// Data shred specific fields
	// Parent offset = 1
	data[DataParentOffsetOffset] = 1
	data[DataParentOffsetOffset+1] = 0

	// Data flags = 0xC5 (block complete, batch complete, tick=5)
	data[DataFlagsOffset] = 0xC5

	// Data size = 150
	data[DataSizeOffset] = 150
	data[DataSizeOffset+1] = 0

	// Payload
	for i := DataPayloadOffset; i < 150; i++ {
		data[i] = byte(i)
	}

	shred, err := Decode(data)
	require.NoError(t, err)

	assert.Equal(t, ShredTypeData, shred.Type)
	assert.Equal(t, AuthTypeLegacy, shred.AuthType)
	assert.Equal(t, uint64(12345678), shred.Slot)
	assert.Equal(t, uint32(42), shred.ShredIndex)
	assert.Equal(t, uint16(1), shred.ShredVersion)
	assert.Equal(t, uint32(40), shred.FECSetIndex)
	assert.Equal(t, uint16(1), shred.ParentOffset)
	assert.True(t, shred.BlockComplete)
	assert.True(t, shred.BatchComplete)
	assert.Equal(t, uint8(5), shred.BatchTick)
	assert.Equal(t, uint16(150), shred.DataSize)
	assert.Equal(t, 150-DataPayloadOffset, len(shred.Payload))
}

func TestDecode_LegacyCodeShred(t *testing.T) {
	data := make([]byte, 200)

	// Variant = 0x5A (Legacy Code)
	data[VariantOffset] = 0x5A

	// Slot = 99999
	data[SlotOffset] = 0x9F
	data[SlotOffset+1] = 0x86
	data[SlotOffset+2] = 0x01
	data[SlotOffset+3] = 0x00

	// Shred index = 100
	data[ShredIndexOffset] = 100

	// Code shred specific fields
	// Num data shreds = 32
	data[CodeNumDataOffset] = 32
	data[CodeNumDataOffset+1] = 0

	// Num coding shreds = 32
	data[CodeNumCodingOffset] = 32
	data[CodeNumCodingOffset+1] = 0

	// Position = 5
	data[CodePositionOffset] = 5
	data[CodePositionOffset+1] = 0

	shred, err := Decode(data)
	require.NoError(t, err)

	assert.Equal(t, ShredTypeCode, shred.Type)
	assert.Equal(t, AuthTypeLegacy, shred.AuthType)
	assert.Equal(t, uint64(99999), shred.Slot)
	assert.Equal(t, uint32(100), shred.ShredIndex)
	assert.Equal(t, uint16(32), shred.NumDataShreds)
	assert.Equal(t, uint16(32), shred.NumCodingShreds)
	assert.Equal(t, uint16(5), shred.Position)
}

func TestDecode_MerkleDataShred(t *testing.T) {
	data := make([]byte, 200)

	// Variant = 0x85 (Merkle Data, tree height 5)
	data[VariantOffset] = 0x85

	data[SlotOffset] = 0x01

	shred, err := Decode(data)
	require.NoError(t, err)

	assert.Equal(t, ShredTypeData, shred.Type)
	assert.Equal(t, AuthTypeMerkle, shred.AuthType)
}

func TestDecode_MerkleCodeShred(t *testing.T) {
	data := make([]byte, 200)

	// Variant = 0x43 (Merkle Code, tree height 3)
	data[VariantOffset] = 0x43

	data[SlotOffset] = 0x01

	shred, err := Decode(data)
	require.NoError(t, err)

	assert.Equal(t, ShredTypeCode, shred.Type)
	assert.Equal(t, AuthTypeMerkle, shred.AuthType)
}

func TestDecode_TooSmall(t *testing.T) {
	data := make([]byte, 10)

	_, err := Decode(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too small")
}

func TestDecode_UnknownVariant(t *testing.T) {
	data := make([]byte, 200)

	// Unknown variant
	data[VariantOffset] = 0xFF

	_, err := Decode(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown shred variant")
}

func TestShred_Summary(t *testing.T) {
	// Data shred
	dataShred := &Shred{
		Type:          ShredTypeData,
		Slot:          12345,
		ShredIndex:    42,
		FECSetIndex:   40,
		BlockComplete: true,
		BatchComplete: false,
		DataSize:      150,
		Payload:       make([]byte, 100),
	}

	summary := dataShred.Summary()
	assert.Contains(t, summary, "[DATA]")
	assert.Contains(t, summary, "slot=12345")
	assert.Contains(t, summary, "idx=42")
	assert.Contains(t, summary, "flags=B")

	// Code shred
	codeShred := &Shred{
		Type:            ShredTypeCode,
		Slot:            12345,
		ShredIndex:      100,
		FECSetIndex:     40,
		NumDataShreds:   32,
		NumCodingShreds: 32,
		Position:        5,
	}

	summary = codeShred.Summary()
	assert.Contains(t, summary, "[CODE]")
	assert.Contains(t, summary, "pos=5/32+32")
}

func TestShred_String(t *testing.T) {
	shred := &Shred{
		Type:          ShredTypeData,
		AuthType:      AuthTypeLegacy,
		Slot:          12345,
		ShredIndex:    42,
		ShredVersion:  1,
		FECSetIndex:   40,
		ParentOffset:  1,
		DataSize:      150,
		BlockComplete: true,
		BatchComplete: true,
		BatchTick:     5,
		Payload:       make([]byte, 100),
	}

	str := shred.String()
	assert.Contains(t, str, "Type:Data")
	assert.Contains(t, str, "Auth:Legacy")
	assert.Contains(t, str, "Slot:12345")
	assert.Contains(t, str, "BlockComplete:true")
}
