// Package shred provides decoding for Solana shred binary format.
// Reference: https://github.com/solana-foundation/specs/blob/main/p2p/shred.md
package shred

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	// ShredMaxSize is the maximum size of a shred packet (1228 bytes).
	ShredMaxSize = 1228

	// Header offsets (common header is 83 bytes / 0x53)
	SignatureOffset   = 0x00
	SignatureSize     = 64
	VariantOffset     = 0x40
	SlotOffset        = 0x41
	ShredIndexOffset  = 0x49
	VersionOffset     = 0x4D
	FECSetIndexOffset = 0x4F
	CommonHeaderEnd   = 0x53

	// Data shred specific offsets (after common header)
	DataParentOffsetOffset = 0x53
	DataFlagsOffset        = 0x55
	DataSizeOffset         = 0x56
	DataPayloadOffset      = 0x58

	// Code shred specific offsets (after common header)
	CodeNumDataOffset   = 0x53
	CodeNumCodingOffset = 0x55
	CodePositionOffset  = 0x57
	CodePayloadOffset   = 0x59
)

// ShredType represents the type of shred.
type ShredType uint8

const (
	ShredTypeUnknown ShredType = iota
	ShredTypeData
	ShredTypeCode
)

func (t ShredType) String() string {
	switch t {
	case ShredTypeData:
		return "Data"
	case ShredTypeCode:
		return "Code"
	default:
		return "Unknown"
	}
}

// AuthType represents the authentication mechanism.
type AuthType uint8

const (
	AuthTypeUnknown AuthType = iota
	AuthTypeLegacy
	AuthTypeMerkle
)

func (a AuthType) String() string {
	switch a {
	case AuthTypeLegacy:
		return "Legacy"
	case AuthTypeMerkle:
		return "Merkle"
	default:
		return "Unknown"
	}
}

// Shred represents a decoded Solana shred.
type Shred struct {
	// Common header fields
	Signature    [64]byte
	Variant      uint8
	Slot         uint64
	ShredIndex   uint32
	ShredVersion uint16
	FECSetIndex  uint32

	// Derived from variant
	Type     ShredType
	AuthType AuthType

	// Data shred specific fields (only valid if Type == ShredTypeData)
	ParentOffset  uint16
	DataFlags     uint8
	DataSize      uint16
	BlockComplete bool
	BatchComplete bool
	BatchTick     uint8

	// Code shred specific fields (only valid if Type == ShredTypeCode)
	NumDataShreds   uint16
	NumCodingShreds uint16
	Position        uint16

	// Payload (the actual data after headers)
	Payload []byte

	// Raw bytes for debugging
	RawSize int
}

// Decode parses a raw shred packet into a Shred struct.
func Decode(data []byte) (*Shred, error) {
	if len(data) < CommonHeaderEnd {
		return nil, fmt.Errorf("shred too small: %d bytes (minimum %d)", len(data), CommonHeaderEnd)
	}

	s := &Shred{
		RawSize: len(data),
	}

	// Parse common header
	copy(s.Signature[:], data[SignatureOffset:SignatureOffset+SignatureSize])
	s.Variant = data[VariantOffset]
	s.Slot = binary.LittleEndian.Uint64(data[SlotOffset:])
	s.ShredIndex = binary.LittleEndian.Uint32(data[ShredIndexOffset:])
	s.ShredVersion = binary.LittleEndian.Uint16(data[VersionOffset:])
	s.FECSetIndex = binary.LittleEndian.Uint32(data[FECSetIndexOffset:])

	// Parse variant to determine shred type and auth mechanism
	s.parseVariant()

	// Parse type-specific fields
	switch s.Type {
	case ShredTypeData:
		if err := s.parseDataShred(data); err != nil {
			return nil, err
		}
	case ShredTypeCode:
		if err := s.parseCodeShred(data); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown shred variant: 0x%02X", s.Variant)
	}

	return s, nil
}

// parseVariant extracts shred type and auth mechanism from the variant byte.
// Variant encoding:
//   - 0x5A (0101 1010): Legacy Code shred
//   - 0xA5 (1010 0101): Legacy Data shred
//   - 0x4X (0100 xxxx): Merkle Code shred (low nibble = merkle tree height)
//   - 0x8X (1000 xxxx): Merkle Data shred (low nibble = merkle tree height)
func (s *Shred) parseVariant() {
	highNibble := (s.Variant >> 4) & 0x0F
	lowNibble := s.Variant & 0x0F

	switch {
	case highNibble == 0x5 && lowNibble == 0xA:
		s.Type = ShredTypeCode
		s.AuthType = AuthTypeLegacy
	case highNibble == 0xA && lowNibble == 0x5:
		s.Type = ShredTypeData
		s.AuthType = AuthTypeLegacy
	case highNibble == 0x4:
		s.Type = ShredTypeCode
		s.AuthType = AuthTypeMerkle
	case highNibble == 0x8:
		s.Type = ShredTypeData
		s.AuthType = AuthTypeMerkle
	default:
		s.Type = ShredTypeUnknown
		s.AuthType = AuthTypeUnknown
	}
}

func (s *Shred) parseDataShred(data []byte) error {
	if len(data) < DataPayloadOffset {
		return errors.New("data shred too small for header")
	}

	s.ParentOffset = binary.LittleEndian.Uint16(data[DataParentOffsetOffset:])
	s.DataFlags = data[DataFlagsOffset]
	s.DataSize = binary.LittleEndian.Uint16(data[DataSizeOffset:])

	// Parse data flags
	s.BlockComplete = (s.DataFlags & 0x80) != 0
	s.BatchComplete = (s.DataFlags & 0x40) != 0
	s.BatchTick = s.DataFlags & 0x3F

	// Extract payload
	if int(s.DataSize) <= len(data) && s.DataSize >= DataPayloadOffset {
		s.Payload = data[DataPayloadOffset:s.DataSize]
	} else if len(data) > DataPayloadOffset {
		s.Payload = data[DataPayloadOffset:]
	}

	return nil
}

func (s *Shred) parseCodeShred(data []byte) error {
	if len(data) < CodePayloadOffset {
		return errors.New("code shred too small for header")
	}

	s.NumDataShreds = binary.LittleEndian.Uint16(data[CodeNumDataOffset:])
	s.NumCodingShreds = binary.LittleEndian.Uint16(data[CodeNumCodingOffset:])
	s.Position = binary.LittleEndian.Uint16(data[CodePositionOffset:])

	// Extract payload
	if len(data) > CodePayloadOffset {
		s.Payload = data[CodePayloadOffset:]
	}

	return nil
}

// SignatureShort returns a shortened hex representation of the signature.
func (s *Shred) SignatureShort() string {
	return hex.EncodeToString(s.Signature[:8]) + "..."
}

// String returns a human-readable representation of the shred.
func (s *Shred) String() string {
	base := fmt.Sprintf("Shred{Type:%s, Auth:%s, Slot:%d, Index:%d, Version:%d, FEC:%d",
		s.Type, s.AuthType, s.Slot, s.ShredIndex, s.ShredVersion, s.FECSetIndex)

	switch s.Type {
	case ShredTypeData:
		return fmt.Sprintf("%s, ParentOff:%d, Size:%d, BlockComplete:%t, BatchComplete:%t, Tick:%d, PayloadLen:%d}",
			base, s.ParentOffset, s.DataSize, s.BlockComplete, s.BatchComplete, s.BatchTick, len(s.Payload))
	case ShredTypeCode:
		return fmt.Sprintf("%s, DataShreds:%d, CodingShreds:%d, Position:%d, PayloadLen:%d}",
			base, s.NumDataShreds, s.NumCodingShreds, s.Position, len(s.Payload))
	default:
		return base + "}"
	}
}

// Summary returns a concise one-line summary suitable for logging.
func (s *Shred) Summary() string {
	switch s.Type {
	case ShredTypeData:
		flags := ""
		if s.BlockComplete {
			flags += "B"
		}
		if s.BatchComplete {
			flags += "E"
		}
		if flags == "" {
			flags = "-"
		}
		return fmt.Sprintf("[DATA] slot=%d idx=%d fec=%d flags=%s size=%d payload=%d",
			s.Slot, s.ShredIndex, s.FECSetIndex, flags, s.DataSize, len(s.Payload))
	case ShredTypeCode:
		return fmt.Sprintf("[CODE] slot=%d idx=%d fec=%d pos=%d/%d+%d",
			s.Slot, s.ShredIndex, s.FECSetIndex, s.Position, s.NumDataShreds, s.NumCodingShreds)
	default:
		return fmt.Sprintf("[????] slot=%d idx=%d variant=0x%02X", s.Slot, s.ShredIndex, s.Variant)
	}
}
