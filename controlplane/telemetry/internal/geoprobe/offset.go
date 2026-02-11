package geoprobe

import (
	"fmt"
	"io"

	bin "github.com/gagliardetto/binary"
)

const (
	MaxReferenceDepth  = 2
	MaxTotalReferences = 5
)

// LocationOffset represents a signed data structure that describes the latency
// relationship between two entities (DZD↔Probe or Probe↔Target).
//
// DZD-generated Offsets contain no references (DZDs are roots of trust).
// Probe-generated Offsets include references to DZD Offsets, enabling targets
// to verify the entire measurement chain.
//
// Based on RFC16: Geolocation Verification
type LocationOffset struct {
	Signature       [64]byte         // Ed25519 signature over the serialized bytes (excluding this field)
	Pubkey          [32]byte         // Signer's public key (DZD or Probe)
	MeasurementSlot uint64           // Current DoubleZero Slot when measurement was taken
	MeasuredRttNs   uint64           // Measured RTT in nanoseconds (minimum observed)
	Lat             float64          // Reference point latitude in WGS84 (decimal degrees)
	Lng             float64          // Reference point longitude in WGS84 (decimal degrees)
	RttNs           uint64           // Accumulated RTT to target in nanoseconds from lat/lng
	NumReferences   uint8            // Number of reference offsets in the chain
	References      []LocationOffset // Reference offsets (recursive chain for verification)
}

// Marshal serializes the LocationOffset to bytes using Borsh encoding.
func (o *LocationOffset) Marshal() ([]byte, error) {
	buf := make([]byte, 0, 256)
	w := &bytesWriter{buf: buf}
	enc := bin.NewBorshEncoder(w)

	if err := enc.Encode(o.Signature); err != nil {
		return nil, fmt.Errorf("failed to encode signature: %w", err)
	}
	if err := enc.Encode(o.Pubkey); err != nil {
		return nil, fmt.Errorf("failed to encode pubkey: %w", err)
	}
	if err := enc.Encode(o.MeasurementSlot); err != nil {
		return nil, fmt.Errorf("failed to encode measurement slot: %w", err)
	}
	if err := enc.Encode(o.Lat); err != nil {
		return nil, fmt.Errorf("failed to encode latitude: %w", err)
	}
	if err := enc.Encode(o.Lng); err != nil {
		return nil, fmt.Errorf("failed to encode longitude: %w", err)
	}
	if err := enc.Encode(o.MeasuredRttNs); err != nil {
		return nil, fmt.Errorf("failed to encode measured rtt: %w", err)
	}
	if err := enc.Encode(o.RttNs); err != nil {
		return nil, fmt.Errorf("failed to encode rtt: %w", err)
	}
	if err := enc.Encode(o.NumReferences); err != nil {
		return nil, fmt.Errorf("failed to encode num references: %w", err)
	}

	for i, ref := range o.References {
		refBytes, err := ref.Marshal()
		if err != nil {
			return nil, fmt.Errorf("failed to encode reference %d: %w", i, err)
		}
		if _, err := w.Write(refBytes); err != nil {
			return nil, fmt.Errorf("failed to write reference %d: %w", i, err)
		}
	}

	return w.buf, nil
}

func (o *LocationOffset) Unmarshal(data []byte) error {
	if err := o.unmarshalHelper(data, nil, 0); err != nil {
		return err
	}

	totalRefs := o.countTotalReferences()
	if totalRefs > MaxTotalReferences {
		return fmt.Errorf("total reference count %d exceeds maximum of %d", totalRefs, MaxTotalReferences)
	}

	return nil
}

// unmarshalHelper uses a shared decoder for recursive unmarshaling of reference chains.
// depth tracks the current nesting level to prevent stack exhaustion.
func (o *LocationOffset) unmarshalHelper(data []byte, dec *bin.Decoder, depth int) error {
	if depth > MaxReferenceDepth {
		return fmt.Errorf("reference chain depth %d exceeds maximum of %d", depth, MaxReferenceDepth)
	}

	ownDec := dec == nil
	if ownDec {
		dec = bin.NewBorshDecoder(data)
	}

	if err := dec.Decode(&o.Signature); err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}
	if err := dec.Decode(&o.Pubkey); err != nil {
		return fmt.Errorf("failed to decode pubkey: %w", err)
	}
	if err := dec.Decode(&o.MeasurementSlot); err != nil {
		return fmt.Errorf("failed to decode measurement slot: %w", err)
	}
	if err := dec.Decode(&o.Lat); err != nil {
		return fmt.Errorf("failed to decode latitude: %w", err)
	}
	if err := dec.Decode(&o.Lng); err != nil {
		return fmt.Errorf("failed to decode longitude: %w", err)
	}
	if err := dec.Decode(&o.MeasuredRttNs); err != nil {
		return fmt.Errorf("failed to decode measured rtt: %w", err)
	}
	if err := dec.Decode(&o.RttNs); err != nil {
		return fmt.Errorf("failed to decode rtt: %w", err)
	}
	if err := dec.Decode(&o.NumReferences); err != nil {
		return fmt.Errorf("failed to decode num references: %w", err)
	}

	o.References = make([]LocationOffset, o.NumReferences)
	for i := uint8(0); i < o.NumReferences; i++ {
		if err := o.References[i].unmarshalHelper(nil, dec, depth+1); err != nil {
			return fmt.Errorf("failed to decode reference %d: %w", i, err)
		}
	}

	return nil
}

// GetSigningBytes returns the bytes that should be signed (excludes the Signature field).
func (o *LocationOffset) GetSigningBytes() ([]byte, error) {
	buf := make([]byte, 0, 256)
	w := &bytesWriter{buf: buf}
	enc := bin.NewBorshEncoder(w)

	if err := enc.Encode(o.Pubkey); err != nil {
		return nil, fmt.Errorf("failed to encode pubkey: %w", err)
	}
	if err := enc.Encode(o.MeasurementSlot); err != nil {
		return nil, fmt.Errorf("failed to encode measurement slot: %w", err)
	}
	if err := enc.Encode(o.Lat); err != nil {
		return nil, fmt.Errorf("failed to encode latitude: %w", err)
	}
	if err := enc.Encode(o.Lng); err != nil {
		return nil, fmt.Errorf("failed to encode longitude: %w", err)
	}
	if err := enc.Encode(o.MeasuredRttNs); err != nil {
		return nil, fmt.Errorf("failed to encode measured rtt: %w", err)
	}
	if err := enc.Encode(o.RttNs); err != nil {
		return nil, fmt.Errorf("failed to encode rtt: %w", err)
	}
	if err := enc.Encode(o.NumReferences); err != nil {
		return nil, fmt.Errorf("failed to encode num references: %w", err)
	}

	for i, ref := range o.References {
		refBytes, err := ref.Marshal()
		if err != nil {
			return nil, fmt.Errorf("failed to encode reference %d: %w", i, err)
		}
		if _, err := w.Write(refBytes); err != nil {
			return nil, fmt.Errorf("failed to write reference %d: %w", i, err)
		}
	}

	return w.buf, nil
}

func (o *LocationOffset) size() (int, error) {
	data, err := o.Marshal()
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// countTotalReferences recursively counts all references in the chain.
func (o *LocationOffset) countTotalReferences() int {
	count := len(o.References)
	for i := range o.References {
		count += o.References[i].countTotalReferences()
	}
	return count
}

type bytesWriter struct {
	buf []byte
}

func (w *bytesWriter) Write(p []byte) (n int, err error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

var _ io.Writer = (*bytesWriter)(nil)
