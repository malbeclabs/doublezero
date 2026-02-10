package geoprobe

import (
	"fmt"
	"io"

	bin "github.com/gagliardetto/binary"
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
	// Ed25519 signature over the serialized bytes (excluding this field)
	Signature [64]byte

	// Signer's public key (DZD or Probe)
	Pubkey [32]byte

	// Current DoubleZero Slot when measurement was taken
	MeasurementSlot uint64

	// Reference point latitude in WGS84 (decimal degrees)
	Lat float64

	// Reference point longitude in WGS84 (decimal degrees)
	Lng float64

	// Measured RTT in nanoseconds (minimum observed)
	MeasuredRttNs uint64

	// Accumulated RTT to target in nanoseconds (from lat/lng)
	RttNs uint64

	// Number of reference offsets in the chain
	NumReferences uint8

	// Reference offsets (recursive chain for verification)
	References []LocationOffset
}

// Marshal serializes the LocationOffset to bytes using Borsh encoding.
// The signature field is included in the output.
func (o *LocationOffset) Marshal() ([]byte, error) {
	buf := make([]byte, 0, 256) // Pre-allocate reasonable size
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

	// Encode references recursively
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

// Unmarshal deserializes bytes into the LocationOffset using Borsh encoding.
func (o *LocationOffset) Unmarshal(data []byte) error {
	return o.unmarshalHelper(data, nil)
}

// unmarshalHelper is a helper function that uses a shared decoder for recursive unmarshaling.
func (o *LocationOffset) unmarshalHelper(data []byte, dec *bin.Decoder) error {
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

	// Decode references recursively using the same decoder
	o.References = make([]LocationOffset, o.NumReferences)
	for i := uint8(0); i < o.NumReferences; i++ {
		if err := o.References[i].unmarshalHelper(nil, dec); err != nil {
			return fmt.Errorf("failed to decode reference %d: %w", i, err)
		}
	}

	return nil
}

// GetSigningBytes returns the bytes that should be signed (all fields except Signature).
func (o *LocationOffset) GetSigningBytes() ([]byte, error) {
	buf := make([]byte, 0, 256)
	w := &bytesWriter{buf: buf}
	enc := bin.NewBorshEncoder(w)

	// Skip signature field, encode everything else
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

	// Encode references recursively
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

// size calculates the serialized size of this LocationOffset.
func (o *LocationOffset) size() (int, error) {
	data, err := o.Marshal()
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// bytesWriter is a simple io.Writer that appends to a byte slice.
type bytesWriter struct {
	buf []byte
}

func (w *bytesWriter) Write(p []byte) (n int, err error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

var _ io.Writer = (*bytesWriter)(nil)
