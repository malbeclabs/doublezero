package signed

import (
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	ProbePacketSize = 108
	ReplyPacketSize = 204

	probePayloadSize = 44  // bytes 0-43: fields signed by sender
	replyPayloadSize = 140 // bytes 0-139: fields signed by reflector
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
type ReplyPacket struct {
	Probe           ProbePacket // Bytes 0-107: Complete original signed probe (echoed)
	ReflectorPubkey [32]byte    // Bytes 108-139: Probe's Ed25519 public key
	Signature       [64]byte    // Bytes 140-203: Ed25519 signature over bytes 0-139
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

func (r *ReplyPacket) Marshal(buf []byte) error {
	if len(buf) < ReplyPacketSize {
		return fmt.Errorf("buffer too small: %d < %d", len(buf), ReplyPacketSize)
	}

	if err := r.Probe.Marshal(buf[0:108]); err != nil {
		return err
	}
	copy(buf[108:140], r.ReflectorPubkey[:])
	copy(buf[140:204], r.Signature[:])
	return nil
}

func UnmarshalReplyPacket(buf []byte) (*ReplyPacket, error) {
	if len(buf) != ReplyPacketSize {
		return nil, errInvalidPacket
	}

	probe, err := UnmarshalProbePacket(buf[0:108])
	if err != nil {
		return nil, err
	}

	r := &ReplyPacket{
		Probe: *probe,
	}
	copy(r.ReflectorPubkey[:], buf[108:140])
	copy(r.Signature[:], buf[140:204])
	return r, nil
}

func NewReplyPacket(probe *ProbePacket, signer Signer) *ReplyPacket {
	pub := signer.Public()

	r := &ReplyPacket{
		Probe: *probe,
	}
	copy(r.ReflectorPubkey[:], pub)

	var payload [replyPayloadSize]byte
	_ = probe.Marshal(payload[0:108])
	copy(payload[108:140], r.ReflectorPubkey[:])

	sig := signer.Sign(payload[:])
	copy(r.Signature[:], sig)

	return r
}

func (r *ReplyPacket) Verify() bool {
	var payload [replyPayloadSize]byte
	_ = r.Probe.Marshal(payload[0:108])
	copy(payload[108:140], r.ReflectorPubkey[:])

	return ed25519.Verify(ed25519.PublicKey(r.ReflectorPubkey[:]), payload[:], r.Signature[:])
}
