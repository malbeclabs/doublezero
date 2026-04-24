package data

import (
	"context"
	"fmt"
	"sort"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type Metro struct {
	PK        solana.PublicKey `json:"pk"`
	Code      string           `json:"code"`
	Name      string           `json:"name"`
	Latitude  float64          `json:"latitude"`
	Longitude float64          `json:"longitude"`
}

type Circuit struct {
	Code        string `json:"code"`
	OriginMetro Metro  `json:"origin_metro"`
	TargetMetro Metro  `json:"target_metro"`
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

	n := len(data.Metros)
	circuits := make([]Circuit, 0, n*(n-1)/2)
	circuitsByCode := make(map[string]struct{})

	for _, originMetro := range data.Metros {
		for _, targetMetro := range data.Metros {
			if originMetro.Code == targetMetro.Code {
				continue
			}

			var origin, target serviceability.Metro
			if originMetro.Code < targetMetro.Code {
				origin, target = originMetro, targetMetro
			} else {
				origin, target = targetMetro, originMetro
			}

			key := circuitKey(origin.Code, target.Code)
			if _, ok := circuitsByCode[key]; ok {
				continue
			}

			circuitsByCode[key] = struct{}{}
			circuits = append(circuits, Circuit{
				Code: key,
				OriginMetro: Metro{
					PK:        origin.PubKey,
					Code:      origin.Code,
					Name:      origin.Name,
					Latitude:  origin.Lat,
					Longitude: origin.Lng,
				},
				TargetMetro: Metro{
					PK:        target.PubKey,
					Code:      target.Code,
					Name:      target.Name,
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
