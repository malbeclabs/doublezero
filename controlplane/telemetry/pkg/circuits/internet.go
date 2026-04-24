package circuits

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type InternetMetroCircuit struct {
	Code        string
	OriginMetro serviceability.Metro
	TargetMetro serviceability.Metro
}

func GetInternetMetroCircuits(ctx context.Context, log *slog.Logger, serviceabilityClient ServiceabilityClient) ([]InternetMetroCircuit, error) {
	data, err := serviceabilityClient.GetProgramData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load serviceability data: %w", err)
	}

	circuits := make([]InternetMetroCircuit, 0, 2*len(data.Links))
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

			key := internetMetroCircuitKey(origin.Code, target.Code)
			if _, ok := circuitsByCode[key]; ok {
				continue
			}

			circuitsByCode[key] = struct{}{}
			circuits = append(circuits, InternetMetroCircuit{
				Code:        key,
				OriginMetro: origin,
				TargetMetro: target,
			})
		}
	}

	sort.Slice(circuits, func(i, j int) bool {
		return circuits[i].Code < circuits[j].Code
	})

	return circuits, nil
}

func internetMetroCircuitKey(origin, target string) string {
	return fmt.Sprintf("%s → %s", origin, target)
}
