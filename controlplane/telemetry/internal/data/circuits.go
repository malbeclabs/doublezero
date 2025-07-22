package data

import (
	"context"
	"fmt"
	"sort"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type Circuit struct {
	Code string `json:"code"`

	OriginDevice serviceability.Device `json:"-"`
	TargetDevice serviceability.Device `json:"-"`
	Link         serviceability.Link   `json:"-"`
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

	for _, link := range data.Links {
		deviceAPK := solana.PublicKeyFromBytes(link.SideAPubKey[:])
		deviceZPK := solana.PublicKeyFromBytes(link.SideZPubKey[:])

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
		circuits = append(circuits, Circuit{
			Code:         forwardKey,
			OriginDevice: deviceA,
			TargetDevice: deviceZ,
			Link:         link,
		})

		// Reverse circuit
		reverseKey := circuitKey(deviceZ.Code, deviceA.Code, link.Code)
		circuits = append(circuits, Circuit{
			Code:         reverseKey,
			OriginDevice: deviceZ,
			TargetDevice: deviceA,
			Link:         link,
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
