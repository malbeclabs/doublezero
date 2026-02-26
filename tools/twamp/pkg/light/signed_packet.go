package twamplight

import (
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"time"
)

const (
	SignedProbePacketSize = 108
	SignedReplyPacketSize = 204

	signedProbePayloadSize = 44  // bytes 0-43: fields signed by sender
	signedReplyPayloadSize = 140 // bytes 0-139: fields signed by reflector
)

// SignedProbePacket is sent from Target to Probe in the inbound probing flow.
// Total size: 108 bytes.
type SignedProbePacket struct {
	Seq          uint32   // Bytes 0-3: Sequence number (big-endian)
	Sec          uint32   // Bytes 4-7: NTP timestamp seconds
	Frac         uint32   // Bytes 8-11: NTP timestamp fractional
	SenderPubkey [32]byte // Bytes 12-43: Target's Ed25519 public key
	Signature    [64]byte // Bytes 44-107: Ed25519 signature over bytes 0-43
}

// SignedReplyPacket is sent from Probe to Target in the inbound probing flow.
// Total size: 204 bytes.
type SignedReplyPacket struct {
	Probe           SignedProbePacket // Bytes 0-107: Complete original signed probe (echoed)
	ReflectorPubkey [32]byte         // Bytes 108-139: Probe's Ed25519 public key
	Signature       [64]byte         // Bytes 140-203: Ed25519 signature over bytes 0-139
}

// NewSignedProbePacket creates a new SignedProbePacket with the given sequence number,
// signed with the provided private key. The public key is embedded in the packet.
func NewSignedProbePacket(seq uint32, privateKey ed25519.PrivateKey) *SignedProbePacket {
	sec, frac := ntpTimestamp(time.Now())
	pub := privateKey.Public().(ed25519.PublicKey)

	p := &SignedProbePacket{
		Seq:  seq,
		Sec:  sec,
		Frac: frac,
	}
	copy(p.SenderPubkey[:], pub)

	// Sign bytes 0-43 (Seq, Sec, Frac, SenderPubkey).
	var payload [signedProbePayloadSize]byte
	binary.BigEndian.PutUint32(payload[0:4], p.Seq)
	binary.BigEndian.PutUint32(payload[4:8], p.Sec)
	binary.BigEndian.PutUint32(payload[8:12], p.Frac)
	copy(payload[12:44], p.SenderPubkey[:])

	sig := ed25519.Sign(privateKey, payload[:])
	copy(p.Signature[:], sig)

	return p
}

// Marshal serializes the SignedProbePacket into buf.
func (p *SignedProbePacket) Marshal(buf []byte) error {
	if len(buf) < SignedProbePacketSize {
		return fmt.Errorf("buffer too small: %d < %d", len(buf), SignedProbePacketSize)
	}

	binary.BigEndian.PutUint32(buf[0:4], p.Seq)
	binary.BigEndian.PutUint32(buf[4:8], p.Sec)
	binary.BigEndian.PutUint32(buf[8:12], p.Frac)
	copy(buf[12:44], p.SenderPubkey[:])
	copy(buf[44:108], p.Signature[:])
	return nil
}

// UnmarshalSignedProbePacket deserializes a SignedProbePacket from buf.
func UnmarshalSignedProbePacket(buf []byte) (*SignedProbePacket, error) {
	if len(buf) != SignedProbePacketSize {
		return nil, ErrInvalidPacket
	}

	p := &SignedProbePacket{
		Seq:  binary.BigEndian.Uint32(buf[0:4]),
		Sec:  binary.BigEndian.Uint32(buf[4:8]),
		Frac: binary.BigEndian.Uint32(buf[8:12]),
	}
	copy(p.SenderPubkey[:], buf[12:44])
	copy(p.Signature[:], buf[44:108])
	return p, nil
}

// VerifyProbe verifies the Ed25519 signature on a SignedProbePacket.
func VerifyProbe(p *SignedProbePacket) bool {
	var payload [signedProbePayloadSize]byte
	binary.BigEndian.PutUint32(payload[0:4], p.Seq)
	binary.BigEndian.PutUint32(payload[4:8], p.Sec)
	binary.BigEndian.PutUint32(payload[8:12], p.Frac)
	copy(payload[12:44], p.SenderPubkey[:])

	return ed25519.Verify(ed25519.PublicKey(p.SenderPubkey[:]), payload[:], p.Signature[:])
}

// Marshal serializes the SignedReplyPacket into buf.
func (r *SignedReplyPacket) Marshal(buf []byte) error {
	if len(buf) < SignedReplyPacketSize {
		return fmt.Errorf("buffer too small: %d < %d", len(buf), SignedReplyPacketSize)
	}

	if err := r.Probe.Marshal(buf[0:108]); err != nil {
		return err
	}
	copy(buf[108:140], r.ReflectorPubkey[:])
	copy(buf[140:204], r.Signature[:])
	return nil
}

// UnmarshalSignedReplyPacket deserializes a SignedReplyPacket from buf.
func UnmarshalSignedReplyPacket(buf []byte) (*SignedReplyPacket, error) {
	if len(buf) != SignedReplyPacketSize {
		return nil, ErrInvalidPacket
	}

	probe, err := UnmarshalSignedProbePacket(buf[0:108])
	if err != nil {
		return nil, err
	}

	r := &SignedReplyPacket{
		Probe: *probe,
	}
	copy(r.ReflectorPubkey[:], buf[108:140])
	copy(r.Signature[:], buf[140:204])
	return r, nil
}

// NewSignedReplyPacket creates a SignedReplyPacket echoing the original probe,
// signed with the reflector's private key.
func NewSignedReplyPacket(probe *SignedProbePacket, privateKey ed25519.PrivateKey) *SignedReplyPacket {
	pub := privateKey.Public().(ed25519.PublicKey)

	r := &SignedReplyPacket{
		Probe: *probe,
	}
	copy(r.ReflectorPubkey[:], pub)

	// Sign bytes 0-139 (Probe + ReflectorPubkey).
	var payload [signedReplyPayloadSize]byte
	_ = probe.Marshal(payload[0:108])
	copy(payload[108:140], r.ReflectorPubkey[:])

	sig := ed25519.Sign(privateKey, payload[:])
	copy(r.Signature[:], sig)

	return r
}

// VerifyReply verifies the Ed25519 signature on a SignedReplyPacket.
func VerifyReply(r *SignedReplyPacket) bool {
	var payload [signedReplyPayloadSize]byte
	_ = r.Probe.Marshal(payload[0:108])
	copy(payload[108:140], r.ReflectorPubkey[:])

	return ed25519.Verify(ed25519.PublicKey(r.ReflectorPubkey[:]), payload[:], r.Signature[:])
}
