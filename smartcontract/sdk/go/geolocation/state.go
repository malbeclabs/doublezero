package geolocation

import (
	"fmt"
	"io"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
)

type AccountType uint8

const (
	AccountTypeNone          AccountType = 0
	AccountTypeProgramConfig AccountType = 1
	AccountTypeGeoProbe      AccountType = 2
)

type GeolocationProgramConfig struct {
	AccountType             AccountType     // 1 byte
	BumpSeed                uint8           // 1 byte
	Version                 uint32          // 4 bytes LE
	MinCompatibleVersion    uint32          // 4 bytes LE
	ServiceabilityProgramID solana.PublicKey // 32 bytes
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
	if err := enc.Encode(g.ServiceabilityProgramID); err != nil {
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
	if err := dec.Decode(&g.ServiceabilityProgramID); err != nil {
		return err
	}
	return nil
}

type GeoProbe struct {
	AccountType        AccountType       // 1 byte
	Owner              solana.PublicKey   // 32 bytes
	BumpSeed           uint8             // 1 byte
	ExchangePK         solana.PublicKey   // 32 bytes
	PublicIP           [4]uint8          // 4 bytes (IPv4 octets)
	LocationOffsetPort uint16            // 2 bytes LE
	Code               string            // 4-byte length prefix + UTF-8 bytes
	ParentDevices      []solana.PublicKey // 4-byte count + N*32 bytes
	MetricsPublisherPK solana.PublicKey   // 32 bytes
	LatencyThresholdNs uint64            // 8 bytes LE
	ReferenceCount     uint32            // 4 bytes LE
}

func (g *GeoProbe) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(g.AccountType); err != nil {
		return err
	}
	if err := enc.Encode(g.Owner); err != nil {
		return err
	}
	if err := enc.Encode(g.BumpSeed); err != nil {
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
	if err := enc.Encode(g.Code); err != nil {
		return err
	}
	if err := enc.Encode(g.ParentDevices); err != nil {
		return err
	}
	if err := enc.Encode(g.MetricsPublisherPK); err != nil {
		return err
	}
	if err := enc.Encode(g.LatencyThresholdNs); err != nil {
		return err
	}
	if err := enc.Encode(g.ReferenceCount); err != nil {
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
	if err := dec.Decode(&g.BumpSeed); err != nil {
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
	if err := dec.Decode(&g.MetricsPublisherPK); err != nil {
		return err
	}
	if err := dec.Decode(&g.LatencyThresholdNs); err != nil {
		return err
	}
	if err := dec.Decode(&g.ReferenceCount); err != nil {
		return err
	}
	return nil
}
