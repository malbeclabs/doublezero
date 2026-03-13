package signed

import (
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	ProbePacketSize    = 108
	LocationOffsetSize = 174 // size of a 0-reference LocationOffset, Borsh-encoded
	MaxOffsets         = 5

	// Probe(108) + AuthorityPubkey(32) + GeoprobePubkey(32) +
	// MeasurementSlot(8) + Lat(8) + Lng(8) + SinceLastRxNs(8) + RttNs(8) + NumOffsets(1)
	replyHeaderSize = 213
	signatureSize   = 64

	MinReplyPacketSize = replyHeaderSize + signatureSize                                 // 277
	MaxReplyPacketSize = replyHeaderSize + MaxOffsets*LocationOffsetSize + signatureSize // 1122

	probePayloadSize = 44 // bytes 0-43: fields signed by sender
)

var errInvalidPacket = errors.New("invalid packet format")

// Signer signs a message with an Ed25519 private key.
type Signer interface {
	Sign(message []byte) []byte
	Public() ed25519.PublicKey
}

// Ed25519Signer implements Signer using a raw Ed25519 private key.
type Ed25519Signer struct {
	key ed25519.PrivateKey
}

func NewEd25519Signer(key ed25519.PrivateKey) *Ed25519Signer {
	return &Ed25519Signer{key: key}
}

func (s *Ed25519Signer) Sign(message []byte) []byte {
	return ed25519.Sign(s.key, message)
}

func (s *Ed25519Signer) Public() ed25519.PublicKey {
	return s.key.Public().(ed25519.PublicKey)
}

func ntpTimestamp(t time.Time) (uint32, uint32) {
	const ntpEpochOffset = 2208988800
	secs := uint32(t.Unix()) + ntpEpochOffset
	nanos := uint64(t.Nanosecond())
	frac := uint32((nanos * (1 << 32)) / 1e9)
	return secs, frac
}

// ProbePacket is sent from Target to Probe in the inbound probing flow.
type ProbePacket struct {
	Seq          uint32   // Bytes 0-3: Sequence number (big-endian)
	Sec          uint32   // Bytes 4-7: NTP timestamp seconds
	Frac         uint32   // Bytes 8-11: NTP timestamp fractional
	SenderPubkey [32]byte // Bytes 12-43: Target's Ed25519 public key
	Signature    [64]byte // Bytes 44-107: Ed25519 signature over bytes 0-43
}

// ReplyPacket is sent from Probe to Target in the inbound probing flow.
// Fields mirror LocationOffset to provide consistent geolocation attestation.
//
// Wire format:
//
//	Bytes 0-107:   Probe (108B)
//	Bytes 108-139: AuthorityPubkey (32B)
//	Bytes 140-171: GeoprobePubkey (32B)
//	Bytes 172-179: MeasurementSlot (8B)
//	Bytes 180-187: Lat (8B)
//	Bytes 188-195: Lng (8B)
//	Bytes 196-203: SinceLastRxNs (8B)
//	Bytes 204-211: RttNs (8B)
//	Byte  212:     NumOffsets (1B)
//	Bytes 213-...: Offset blobs (N × LocationOffsetSize bytes)
//	Bytes ...-end: Signature (64B) over all preceding bytes
type ReplyPacket struct {
	Probe           ProbePacket
	AuthorityPubkey [32]byte
	GeoprobePubkey  [32]byte
	MeasurementSlot uint64   // DoubleZero slot from reference offset
	Lat             float64  // Reference point latitude (WGS84 decimal degrees)
	Lng             float64  // Reference point longitude (WGS84 decimal degrees)
	SinceLastRxNs   uint64   // Nanoseconds between reflector Tx (reply N-1) and Rx (probe N); approximates network RTT
	RttNs           uint64   // Accumulated RTT from Lat/Lng in nanoseconds
	Offsets         [][]byte // 0-5 opaque offset blobs, each exactly LocationOffsetSize bytes
	Signature       [64]byte
}

// Size returns the wire size of the reply packet.
func (r *ReplyPacket) Size() int {
	return replyHeaderSize + len(r.Offsets)*LocationOffsetSize + signatureSize
}

func NewProbePacket(seq uint32, signer Signer) *ProbePacket {
	sec, frac := ntpTimestamp(time.Now())
	pub := signer.Public()

	p := &ProbePacket{
		Seq:  seq,
		Sec:  sec,
		Frac: frac,
	}
	copy(p.SenderPubkey[:], pub)

	var payload [probePayloadSize]byte
	binary.BigEndian.PutUint32(payload[0:4], p.Seq)
	binary.BigEndian.PutUint32(payload[4:8], p.Sec)
	binary.BigEndian.PutUint32(payload[8:12], p.Frac)
	copy(payload[12:44], p.SenderPubkey[:])

	sig := signer.Sign(payload[:])
	copy(p.Signature[:], sig)

	return p
}

func (p *ProbePacket) Marshal(buf []byte) error {
	if len(buf) < ProbePacketSize {
		return fmt.Errorf("buffer too small: %d < %d", len(buf), ProbePacketSize)
	}

	binary.BigEndian.PutUint32(buf[0:4], p.Seq)
	binary.BigEndian.PutUint32(buf[4:8], p.Sec)
	binary.BigEndian.PutUint32(buf[8:12], p.Frac)
	copy(buf[12:44], p.SenderPubkey[:])
	copy(buf[44:108], p.Signature[:])
	return nil
}

func UnmarshalProbePacket(buf []byte) (*ProbePacket, error) {
	if len(buf) != ProbePacketSize {
		return nil, errInvalidPacket
	}

	p := &ProbePacket{
		Seq:  binary.BigEndian.Uint32(buf[0:4]),
		Sec:  binary.BigEndian.Uint32(buf[4:8]),
		Frac: binary.BigEndian.Uint32(buf[8:12]),
	}
	copy(p.SenderPubkey[:], buf[12:44])
	copy(p.Signature[:], buf[44:108])
	return p, nil
}

func (p *ProbePacket) Verify() bool {
	var payload [probePayloadSize]byte
	binary.BigEndian.PutUint32(payload[0:4], p.Seq)
	binary.BigEndian.PutUint32(payload[4:8], p.Sec)
	binary.BigEndian.PutUint32(payload[8:12], p.Frac)
	copy(payload[12:44], p.SenderPubkey[:])

	return ed25519.Verify(ed25519.PublicKey(p.SenderPubkey[:]), payload[:], p.Signature[:])
}

// marshalPayload writes the signed portion of the reply (everything before the
// signature) into buf and returns the number of bytes written.
func (r *ReplyPacket) marshalPayload(buf []byte) (int, error) {
	payloadSize := replyHeaderSize + len(r.Offsets)*LocationOffsetSize
	if len(buf) < payloadSize {
		return 0, fmt.Errorf("buffer too small: %d < %d", len(buf), payloadSize)
	}

	if err := r.Probe.Marshal(buf[0:108]); err != nil {
		return 0, err
	}
	copy(buf[108:140], r.AuthorityPubkey[:])
	copy(buf[140:172], r.GeoprobePubkey[:])
	binary.BigEndian.PutUint64(buf[172:180], r.MeasurementSlot)
	binary.BigEndian.PutUint64(buf[180:188], math.Float64bits(r.Lat))
	binary.BigEndian.PutUint64(buf[188:196], math.Float64bits(r.Lng))
	binary.BigEndian.PutUint64(buf[196:204], r.SinceLastRxNs)
	binary.BigEndian.PutUint64(buf[204:212], r.RttNs)
	buf[212] = uint8(len(r.Offsets))

	off := replyHeaderSize
	for _, blob := range r.Offsets {
		copy(buf[off:off+LocationOffsetSize], blob)
		off += LocationOffsetSize
	}

	return payloadSize, nil
}

func (r *ReplyPacket) Marshal(buf []byte) (int, error) {
	size := r.Size()
	if len(buf) < size {
		return 0, fmt.Errorf("buffer too small: %d < %d", len(buf), size)
	}

	payloadSize, err := r.marshalPayload(buf)
	if err != nil {
		return 0, err
	}
	copy(buf[payloadSize:payloadSize+signatureSize], r.Signature[:])
	return size, nil
}

func UnmarshalReplyPacket(buf []byte) (*ReplyPacket, error) {
	if len(buf) < MinReplyPacketSize {
		return nil, errInvalidPacket
	}

	numOffsets := int(buf[212])
	if numOffsets > MaxOffsets {
		return nil, errInvalidPacket
	}

	expectedSize := replyHeaderSize + numOffsets*LocationOffsetSize + signatureSize
	if len(buf) != expectedSize {
		return nil, errInvalidPacket
	}

	probe, err := UnmarshalProbePacket(buf[0:108])
	if err != nil {
		return nil, err
	}

	r := &ReplyPacket{
		Probe:           *probe,
		MeasurementSlot: binary.BigEndian.Uint64(buf[172:180]),
		Lat:             math.Float64frombits(binary.BigEndian.Uint64(buf[180:188])),
		Lng:             math.Float64frombits(binary.BigEndian.Uint64(buf[188:196])),
		SinceLastRxNs:   binary.BigEndian.Uint64(buf[196:204]),
		RttNs:           binary.BigEndian.Uint64(buf[204:212]),
	}
	copy(r.AuthorityPubkey[:], buf[108:140])
	copy(r.GeoprobePubkey[:], buf[140:172])

	if numOffsets > 0 {
		r.Offsets = make([][]byte, numOffsets)
		off := replyHeaderSize
		for i := range numOffsets {
			r.Offsets[i] = make([]byte, LocationOffsetSize)
			copy(r.Offsets[i], buf[off:off+LocationOffsetSize])
			off += LocationOffsetSize
		}
	}

	sigStart := len(buf) - signatureSize
	copy(r.Signature[:], buf[sigStart:])
	return r, nil
}

func NewReplyPacket(probe *ProbePacket, signer Signer, geoprobePubkey [32]byte, offsets [][]byte, slot uint64, lat, lng float64, sinceLastRxNs, rttNs uint64) (*ReplyPacket, error) {
	if len(offsets) > MaxOffsets {
		return nil, fmt.Errorf("too many offsets: %d > %d", len(offsets), MaxOffsets)
	}
	for i, blob := range offsets {
		if len(blob) != LocationOffsetSize {
			return nil, fmt.Errorf("offset %d has wrong size: %d != %d", i, len(blob), LocationOffsetSize)
		}
	}

	pub := signer.Public()

	r := &ReplyPacket{
		Probe:           *probe,
		GeoprobePubkey:  geoprobePubkey,
		MeasurementSlot: slot,
		Lat:             lat,
		Lng:             lng,
		SinceLastRxNs:   sinceLastRxNs,
		RttNs:           rttNs,
		Offsets:         offsets,
	}
	copy(r.AuthorityPubkey[:], pub)

	payloadSize := replyHeaderSize + len(offsets)*LocationOffsetSize
	payload := make([]byte, payloadSize)
	if _, err := r.marshalPayload(payload); err != nil {
		return nil, err
	}

	sig := signer.Sign(payload)
	copy(r.Signature[:], sig)

	return r, nil
}

func (r *ReplyPacket) Verify() bool {
	payloadSize := replyHeaderSize + len(r.Offsets)*LocationOffsetSize
	payload := make([]byte, payloadSize)
	if _, err := r.marshalPayload(payload); err != nil {
		return false
	}

	return ed25519.Verify(ed25519.PublicKey(r.AuthorityPubkey[:]), payload, r.Signature[:])
}

// OffsetInfo holds the fields extracted from a Borsh-encoded LocationOffset blob
// that the reflector needs for populating reply packets.
type OffsetInfo struct {
	MeasurementSlot uint64
	Lat             float64
	Lng             float64
	RttNs           uint64
	TargetIP        [4]byte
}

// ParseOffsetInfo extracts location and timing fields from a Borsh-encoded
// LocationOffset blob at known byte positions (little-endian).
//
// Byte layout (v1): Signature(64) + Version(1) + AuthorityPubkey(32) +
// SenderPubkey(32) + MeasurementSlot(8) + Lat(8) + Lng(8) + MeasuredRttNs(8) +
// RttNs(8) + TargetIP(4) + NumReferences(1) = 174 bytes
func ParseOffsetInfo(blob []byte) (OffsetInfo, bool) {
	if len(blob) < LocationOffsetSize {
		return OffsetInfo{}, false
	}
	targetIP := [4]byte(blob[169:173])
	return OffsetInfo{
		MeasurementSlot: binary.LittleEndian.Uint64(blob[129:137]),
		Lat:             math.Float64frombits(binary.LittleEndian.Uint64(blob[137:145])),
		Lng:             math.Float64frombits(binary.LittleEndian.Uint64(blob[145:153])),
		RttNs:           binary.LittleEndian.Uint64(blob[161:169]),
		TargetIP:        targetIP,
	}, true
}
