package circuits

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

type InternetExchangeCircuit struct {
	Code           string
	OriginExchange serviceability.Exchange
	TargetExchange serviceability.Exchange
}

func GetInternetExchangeCircuits(ctx context.Context, log *slog.Logger, serviceabilityClient ServiceabilityClient) ([]InternetExchangeCircuit, error) {
	data, err := serviceabilityClient.GetProgramData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load serviceability data: %w", err)
	}

	circuits := make([]InternetExchangeCircuit, 0, 2*len(data.Links))
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

			key := internetExchangeCircuitKey(origin.Code, target.Code)
			if _, ok := circuitsByCode[key]; ok {
				continue
			}

			circuitsByCode[key] = struct{}{}
			circuits = append(circuits, InternetExchangeCircuit{
				Code:           key,
				OriginExchange: origin,
				TargetExchange: target,
			})
		}
	}

	sort.Slice(circuits, func(i, j int) bool {
		return circuits[i].Code < circuits[j].Code
	})

	return circuits, nil
}

func internetExchangeCircuitKey(origin, target string) string {
	return fmt.Sprintf("%s → %s", origin, target)
}
