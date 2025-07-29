package data

import (
	"context"
	"fmt"
	"sort"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type Device struct {
	PK       solana.PublicKey `json:"pk"`
	Code     string           `json:"code"`
	Location Location         `json:"location"`
}

type Link struct {
	PK   solana.PublicKey `json:"pk"`
	Code string           `json:"code"`
}

type Location struct {
	PK        solana.PublicKey `json:"pk"`
	Name      string           `json:"name"`
	Country   string           `json:"country"`
	Latitude  float64          `json:"latitude"`
	Longitude float64          `json:"longitude"`
}

type Circuit struct {
	Code         string `json:"code"`
	OriginDevice Device `json:"origin_device"`
	TargetDevice Device `json:"target_device"`
	Link         Link   `json:"link"`
}

func (p *provider) GetCircuits(ctx context.Context) ([]Circuit, error) {
	cached := p.GetCachedCircuits(ctx)
	if cached != nil {
		return cached, nil
	}

	data, err := p.cfg.ServiceabilityClient.GetProgramData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load serviceability data: %w", err)
	}

	circuits := make([]Circuit, 0, 2*len(data.Links))

	devicesByPK := map[string]serviceability.Device{}
	for _, device := range data.Devices {
		pk := solana.PublicKeyFromBytes(device.PubKey[:])
		devicesByPK[pk.String()] = device
	}

	locationsByPK := map[string]serviceability.Location{}
	for _, location := range data.Locations {
		pk := solana.PublicKeyFromBytes(location.PubKey[:])
		locationsByPK[pk.String()] = location
	}

	for _, link := range data.Links {
		deviceAPK := solana.PublicKeyFromBytes(link.SideAPubKey[:])
		deviceZPK := solana.PublicKeyFromBytes(link.SideZPubKey[:])
		linkPK := solana.PublicKeyFromBytes(link.PubKey[:])

		deviceA, ok := devicesByPK[deviceAPK.String()]
		if !ok {
			p.cfg.Logger.Warn("device A not found, skipping link", "link_code", link.Code, "device_a_pk", deviceAPK.String())
			continue
		}
		deviceZ, ok := devicesByPK[deviceZPK.String()]
		if !ok {
			p.cfg.Logger.Warn("device Z not found, skipping link", "link_code", link.Code, "device_z_pk", deviceZPK.String())
			continue
		}

		// Forward circuit
		forwardKey := circuitKey(deviceA.Code, deviceZ.Code, link.Code)
		originLocation := locationsByPK[solana.PublicKeyFromBytes(deviceA.LocationPubKey[:]).String()]
		targetLocation := locationsByPK[solana.PublicKeyFromBytes(deviceZ.LocationPubKey[:]).String()]
		circuits = append(circuits, Circuit{
			Code: forwardKey,
			OriginDevice: Device{
				PK:   deviceAPK,
				Code: deviceA.Code,
				Location: Location{
					PK:        solana.PublicKeyFromBytes(originLocation.PubKey[:]),
					Name:      originLocation.Name,
					Country:   originLocation.Country,
					Latitude:  originLocation.Lat,
					Longitude: originLocation.Lng,
				},
			},
			TargetDevice: Device{
				PK:   deviceZPK,
				Code: deviceZ.Code,
				Location: Location{
					PK:        solana.PublicKeyFromBytes(targetLocation.PubKey[:]),
					Name:      targetLocation.Name,
					Country:   targetLocation.Country,
					Latitude:  targetLocation.Lat,
					Longitude: targetLocation.Lng,
				},
			},
			Link: Link{
				PK:   linkPK,
				Code: link.Code,
			},
		})

		// Reverse circuit
		reverseKey := circuitKey(deviceZ.Code, deviceA.Code, link.Code)
		originLocation = locationsByPK[solana.PublicKeyFromBytes(deviceZ.LocationPubKey[:]).String()]
		targetLocation = locationsByPK[solana.PublicKeyFromBytes(deviceA.LocationPubKey[:]).String()]
		circuits = append(circuits, Circuit{
			Code: reverseKey,
			OriginDevice: Device{
				PK:   deviceZPK,
				Code: deviceZ.Code,
				Location: Location{
					PK:        solana.PublicKeyFromBytes(targetLocation.PubKey[:]),
					Name:      targetLocation.Name,
					Country:   targetLocation.Country,
					Latitude:  targetLocation.Lat,
					Longitude: targetLocation.Lng,
				},
			},
			TargetDevice: Device{
				PK:   deviceAPK,
				Code: deviceA.Code,
				Location: Location{
					PK:        solana.PublicKeyFromBytes(originLocation.PubKey[:]),
					Name:      originLocation.Name,
					Country:   originLocation.Country,
					Latitude:  originLocation.Lat,
					Longitude: originLocation.Lng,
				},
			},
			Link: Link{
				PK:   linkPK,
				Code: link.Code,
			},
		})
	}

	sort.Slice(circuits, func(i, j int) bool {
		return circuits[i].Code < circuits[j].Code
	})

	p.SetCachedCircuits(ctx, circuits)

	return circuits, nil
}

func circuitKey(origin, target, link string) string {
	return fmt.Sprintf("%s → %s (%s)", origin, target, link)
}
