package geolocation

import (
	"encoding/binary"
	"fmt"
	"io"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
)

type AccountType uint8

const (
	AccountTypeNone            AccountType = 0
	AccountTypeProgramConfig   AccountType = 1
	AccountTypeGeoProbe        AccountType = 2
	AccountTypeGeolocationUser AccountType = 3
)

type GeolocationProgramConfig struct {
	AccountType          AccountType // 1 byte
	BumpSeed             uint8       // 1 byte
	Version              uint32      // 4 bytes LE
	MinCompatibleVersion uint32      // 4 bytes LE
}

func (g *GeolocationProgramConfig) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(g.AccountType); err != nil {
		return err
	}
	if err := enc.Encode(g.BumpSeed); err != nil {
		return err
	}
	if err := enc.Encode(g.Version); err != nil {
		return err
	}
	if err := enc.Encode(g.MinCompatibleVersion); err != nil {
		return err
	}
	return nil
}

func (g *GeolocationProgramConfig) Deserialize(data []byte) error {
	dec := bin.NewBorshDecoder(data)
	if err := dec.Decode(&g.AccountType); err != nil {
		return err
	}
	if err := dec.Decode(&g.BumpSeed); err != nil {
		return err
	}
	if err := dec.Decode(&g.Version); err != nil {
		return err
	}
	if err := dec.Decode(&g.MinCompatibleVersion); err != nil {
		return err
	}
	return nil
}

type KeyedGeoProbe struct {
	Pubkey solana.PublicKey
	GeoProbe
}

type GeoProbe struct {
	AccountType        AccountType        // 1 byte
	Owner              solana.PublicKey   // 32 bytes
	ExchangePK         solana.PublicKey   // 32 bytes
	PublicIP           [4]uint8           // 4 bytes (IPv4 octets)
	LocationOffsetPort uint16             // 2 bytes LE
	MetricsPublisherPK solana.PublicKey   // 32 bytes
	ReferenceCount     uint32             // 4 bytes LE
	Code               string             // 4-byte length prefix + UTF-8 bytes
	ParentDevices      []solana.PublicKey // 4-byte count + N*32 bytes
	TargetUpdateCount  uint32             // 4 bytes LE (appended; defaults to 0 for old accounts)
}

func (g *GeoProbe) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(g.AccountType); err != nil {
		return err
	}
	if err := enc.Encode(g.Owner); err != nil {
		return err
	}
	if err := enc.Encode(g.ExchangePK); err != nil {
		return err
	}
	if err := enc.Encode(g.PublicIP); err != nil {
		return err
	}
	if err := enc.Encode(g.LocationOffsetPort); err != nil {
		return err
	}
	if err := enc.Encode(g.MetricsPublisherPK); err != nil {
		return err
	}
	if err := enc.Encode(g.ReferenceCount); err != nil {
		return err
	}
	if err := enc.Encode(g.Code); err != nil {
		return err
	}
	if err := enc.Encode(g.ParentDevices); err != nil {
		return err
	}
	if err := enc.Encode(g.TargetUpdateCount); err != nil {
		return err
	}
	return nil
}

func (g *GeoProbe) Deserialize(data []byte) error {
	dec := bin.NewBorshDecoder(data)
	if err := dec.Decode(&g.AccountType); err != nil {
		return err
	}
	if err := dec.Decode(&g.Owner); err != nil {
		return err
	}
	if err := dec.Decode(&g.ExchangePK); err != nil {
		return err
	}
	if err := dec.Decode(&g.PublicIP); err != nil {
		return err
	}
	if err := dec.Decode(&g.LocationOffsetPort); err != nil {
		return err
	}
	if err := dec.Decode(&g.MetricsPublisherPK); err != nil {
		return err
	}
	if err := dec.Decode(&g.ReferenceCount); err != nil {
		return err
	}
	if err := dec.Decode(&g.Code); err != nil {
		return err
	}
	if len(g.Code) > MaxCodeLength {
		return fmt.Errorf("code length %d exceeds max allowed length %d", len(g.Code), MaxCodeLength)
	}
	if err := dec.Decode(&g.ParentDevices); err != nil {
		return err
	}
	if len(g.ParentDevices) > MaxParentDevices {
		return fmt.Errorf("parent devices count %d exceeds max allowed %d", len(g.ParentDevices), MaxParentDevices)
	}
	// TargetUpdateCount is appended; old accounts without it default to 0.
	if err := dec.Decode(&g.TargetUpdateCount); err != nil {
		g.TargetUpdateCount = 0
	}
	return nil
}

type GeolocationPaymentStatus uint8

const (
	GeolocationPaymentStatusDelinquent GeolocationPaymentStatus = 0
	GeolocationPaymentStatusPaid       GeolocationPaymentStatus = 1
)

func (s GeolocationPaymentStatus) String() string {
	switch s {
	case GeolocationPaymentStatusDelinquent:
		return "delinquent"
	case GeolocationPaymentStatusPaid:
		return "paid"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

type GeolocationUserStatus uint8

const (
	GeolocationUserStatusActivated GeolocationUserStatus = 0
	GeolocationUserStatusSuspended GeolocationUserStatus = 1
)

func (s GeolocationUserStatus) String() string {
	switch s {
	case GeolocationUserStatusActivated:
		return "activated"
	case GeolocationUserStatusSuspended:
		return "suspended"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

type GeoLocationTargetType uint8

const (
	GeoLocationTargetTypeOutbound GeoLocationTargetType = 0
	GeoLocationTargetTypeInbound  GeoLocationTargetType = 1
)

func (t GeoLocationTargetType) String() string {
	switch t {
	case GeoLocationTargetTypeOutbound:
		return "outbound"
	case GeoLocationTargetTypeInbound:
		return "inbound"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

type FlatPerEpochConfig struct {
	Rate                 uint64 // 8 bytes LE
	LastDeductionDzEpoch uint64 // 8 bytes LE
}

func (f *FlatPerEpochConfig) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(f.Rate); err != nil {
		return err
	}
	if err := enc.Encode(f.LastDeductionDzEpoch); err != nil {
		return err
	}
	return nil
}

func (f *FlatPerEpochConfig) Deserialize(dec *bin.Decoder) error {
	if err := dec.Decode(&f.Rate); err != nil {
		return err
	}
	if err := dec.Decode(&f.LastDeductionDzEpoch); err != nil {
		return err
	}
	return nil
}

const (
	BillingConfigFlatPerEpoch uint8 = 0
)

type GeolocationBillingConfig struct {
	Variant      uint8              // discriminant byte
	FlatPerEpoch FlatPerEpochConfig // only valid when Variant == BillingConfigFlatPerEpoch
}

func (b *GeolocationBillingConfig) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(b.Variant); err != nil {
		return err
	}
	switch b.Variant {
	case BillingConfigFlatPerEpoch:
		return b.FlatPerEpoch.Serialize(w)
	default:
		return fmt.Errorf("unknown billing config variant: %d", b.Variant)
	}
}

func (b *GeolocationBillingConfig) Deserialize(dec *bin.Decoder) error {
	if err := dec.Decode(&b.Variant); err != nil {
		return err
	}
	switch b.Variant {
	case BillingConfigFlatPerEpoch:
		return b.FlatPerEpoch.Deserialize(dec)
	default:
		return fmt.Errorf("unknown billing config variant: %d", b.Variant)
	}
}

type GeolocationTarget struct {
	TargetType         GeoLocationTargetType // 1 byte
	IPAddress          [4]uint8              // 4 bytes (IPv4 octets)
	LocationOffsetPort uint16                // 2 bytes LE
	TargetPK           solana.PublicKey      // 32 bytes
	GeoProbePK         solana.PublicKey      // 32 bytes
}

func (t *GeolocationTarget) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(t.TargetType); err != nil {
		return err
	}
	if err := enc.Encode(t.IPAddress); err != nil {
		return err
	}
	if err := enc.Encode(t.LocationOffsetPort); err != nil {
		return err
	}
	if err := enc.Encode(t.TargetPK); err != nil {
		return err
	}
	if err := enc.Encode(t.GeoProbePK); err != nil {
		return err
	}
	return nil
}

func (t *GeolocationTarget) Deserialize(dec *bin.Decoder) error {
	if err := dec.Decode(&t.TargetType); err != nil {
		return err
	}
	if t.TargetType > GeoLocationTargetTypeInbound {
		return fmt.Errorf("invalid target type: %d", t.TargetType)
	}
	if err := dec.Decode(&t.IPAddress); err != nil {
		return err
	}
	if err := dec.Decode(&t.LocationOffsetPort); err != nil {
		return err
	}
	if err := dec.Decode(&t.TargetPK); err != nil {
		return err
	}
	if err := dec.Decode(&t.GeoProbePK); err != nil {
		return err
	}
	return nil
}

type KeyedGeolocationUser struct {
	Pubkey solana.PublicKey
	GeolocationUser
}

type GeolocationUser struct {
	AccountType   AccountType              // 1 byte
	Owner         solana.PublicKey         // 32 bytes
	Code          string                   // 4-byte length prefix + UTF-8 bytes
	TokenAccount  solana.PublicKey         // 32 bytes
	PaymentStatus GeolocationPaymentStatus // 1 byte
	Billing       GeolocationBillingConfig // 1 + 16 = 17 bytes
	Status        GeolocationUserStatus    // 1 byte
	Targets       []GeolocationTarget      // 4-byte count + 71*N bytes
}

func (g *GeolocationUser) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(g.AccountType); err != nil {
		return err
	}
	if err := enc.Encode(g.Owner); err != nil {
		return err
	}
	if err := enc.Encode(g.Code); err != nil {
		return err
	}
	if err := enc.Encode(g.TokenAccount); err != nil {
		return err
	}
	if err := enc.Encode(g.PaymentStatus); err != nil {
		return err
	}
	if err := g.Billing.Serialize(w); err != nil {
		return err
	}
	if err := enc.Encode(g.Status); err != nil {
		return err
	}
	// Serialize targets: 4-byte length prefix + each target
	if err := enc.Encode(uint32(len(g.Targets))); err != nil {
		return err
	}
	for i := range g.Targets {
		if err := g.Targets[i].Serialize(w); err != nil {
			return err
		}
	}
	return nil
}

// geolocationBillingConfigSize is the wire size of a GeolocationBillingConfig (1+8+8).
const geolocationBillingConfigSize = 17

// geolocationUserTargetSize is the wire size of a single GeolocationTarget (1+4+2+32+32).
const geolocationUserTargetSize = 71

func (g *GeolocationUser) Deserialize(data []byte) error {
	// Pre-validate the code string length prefix from raw bytes before the
	// decoder allocates memory for it. The prefix sits at a fixed offset:
	// account_type(1) + owner(32) = 33 bytes.
	const codeOffset = 1 + 32
	if len(data) < codeOffset+4 {
		return fmt.Errorf("data too short for code length prefix: %d bytes", len(data))
	}
	codeLen := binary.LittleEndian.Uint32(data[codeOffset : codeOffset+4])
	if codeLen > MaxCodeLength {
		return fmt.Errorf("code length %d exceeds max allowed length %d", codeLen, MaxCodeLength)
	}

	// Pre-validate the target count from raw bytes. The count sits after:
	// code_offset(33) + code_len_prefix(4) + code(codeLen) + token_account(32) +
	// payment_status(1) + billing + status(1) = 88 + codeLen.
	targetCountOffset := codeOffset + 4 + int(codeLen) + 32 + 1 + geolocationBillingConfigSize + 1
	if len(data) < targetCountOffset+4 {
		return fmt.Errorf("data too short for target count: %d bytes", len(data))
	}
	targetCount := binary.LittleEndian.Uint32(data[targetCountOffset : targetCountOffset+4])
	if targetCount > MaxTargets {
		return fmt.Errorf("targets count %d exceeds max allowed %d", targetCount, MaxTargets)
	}
	if len(data) < targetCountOffset+4+int(targetCount)*geolocationUserTargetSize {
		return fmt.Errorf("data too short for %d targets: need %d bytes, have %d",
			targetCount, targetCountOffset+4+int(targetCount)*geolocationUserTargetSize, len(data))
	}

	dec := bin.NewBorshDecoder(data)
	if err := dec.Decode(&g.AccountType); err != nil {
		return err
	}
	if err := dec.Decode(&g.Owner); err != nil {
		return err
	}
	if err := dec.Decode(&g.Code); err != nil {
		return err
	}
	if err := dec.Decode(&g.TokenAccount); err != nil {
		return err
	}
	if err := dec.Decode(&g.PaymentStatus); err != nil {
		return err
	}
	if g.PaymentStatus > GeolocationPaymentStatusPaid {
		return fmt.Errorf("invalid payment status: %d", g.PaymentStatus)
	}
	if err := g.Billing.Deserialize(dec); err != nil {
		return err
	}
	if err := dec.Decode(&g.Status); err != nil {
		return err
	}
	if g.Status > GeolocationUserStatusSuspended {
		return fmt.Errorf("invalid user status: %d", g.Status)
	}
	// Skip the target count — already validated above.
	var tc uint32
	if err := dec.Decode(&tc); err != nil {
		return err
	}
	g.Targets = make([]GeolocationTarget, targetCount)
	for i := uint32(0); i < targetCount; i++ {
		if err := g.Targets[i].Deserialize(dec); err != nil {
			return err
		}
	}
	return nil
}
