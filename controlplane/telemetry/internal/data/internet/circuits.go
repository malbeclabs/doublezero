package data

import (
	"context"
	"fmt"
	"sort"

	"github.com/gagliardetto/solana-go"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

type Exchange struct {
	PK        solana.PublicKey `json:"pk"`
	Code      string           `json:"code"`
	Name      string           `json:"name"`
	Latitude  float64          `json:"latitude"`
	Longitude float64          `json:"longitude"`
}

type Circuit struct {
	Code           string   `json:"code"`
	OriginExchange Exchange `json:"origin_exchange"`
	TargetExchange Exchange `json:"target_exchange"`
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

	n := len(data.Exchanges)
	circuits := make([]Circuit, 0, n*(n-1)/2)
	circuitsByCode := make(map[string]struct{})

	for _, originExchange := range data.Exchanges {
		for _, targetExchange := range data.Exchanges {
			if originExchange.Code == targetExchange.Code {
				continue
			}

			var origin, target serviceability.Exchange
			if originExchange.Code < targetExchange.Code {
				origin, target = originExchange, targetExchange
			} else {
				origin, target = targetExchange, originExchange
			}

			key := circuitKey(origin.Code, target.Code)
			if _, ok := circuitsByCode[key]; ok {
				continue
			}

			circuitsByCode[key] = struct{}{}
			circuits = append(circuits, Circuit{
				Code: key,
				OriginExchange: Exchange{
					PK:        origin.PubKey,
					Code:      origin.Code,
					Name:      origin.Name,
					Latitude:  origin.Lat,
					Longitude: origin.Lng,
				},
				TargetExchange: Exchange{
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
