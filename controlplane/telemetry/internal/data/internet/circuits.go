package data

import (
	"context"
	"fmt"
	"sort"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type Location struct {
	PK        solana.PublicKey `json:"pk"`
	Code      string           `json:"code"`
	Name      string           `json:"name"`
	Country   string           `json:"country"`
	Latitude  float64          `json:"latitude"`
	Longitude float64          `json:"longitude"`
}

type Circuit struct {
	Code           string   `json:"code"`
	OriginLocation Location `json:"origin_location"`
	TargetLocation Location `json:"target_location"`
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
	circuitsByCode := make(map[string]struct{})

	for _, originLocation := range data.Locations {
		for _, targetLocation := range data.Locations {
			if originLocation.Code == targetLocation.Code {
				continue
			}

			var origin, target serviceability.Location
			if originLocation.Code < targetLocation.Code {
				origin, target = originLocation, targetLocation
			} else {
				origin, target = targetLocation, originLocation
			}

			key := circuitKey(origin.Code, target.Code)
			if _, ok := circuitsByCode[key]; ok {
				continue
			}

			circuitsByCode[key] = struct{}{}
			circuits = append(circuits, Circuit{
				Code: key,
				OriginLocation: Location{
					PK:        origin.PubKey,
					Code:      origin.Code,
					Name:      origin.Name,
					Country:   origin.Country,
					Latitude:  origin.Lat,
					Longitude: origin.Lng,
				},
				TargetLocation: Location{
					PK:        target.PubKey,
					Code:      target.Code,
					Name:      target.Name,
					Country:   target.Country,
					Latitude:  target.Lat,
					Longitude: target.Lng,
				},
			})
		}
	}

	sort.Slice(circuits, func(i, j int) bool {
		return circuits[i].Code < circuits[j].Code
	})

	p.SetCachedCircuits(ctx, circuits)

	return circuits, nil
}

func circuitKey(origin, target string) string {
	return fmt.Sprintf("%s → %s", origin, target)
}
