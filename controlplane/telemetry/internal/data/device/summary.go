package data

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

func (p *provider) GetSummaryForCircuits(ctx context.Context, cfg GetSummaryForCircuitsConfig) ([]CircuitSummary, error) {
	output := []CircuitSummary{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	circuits, err := p.GetCircuits(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get circuits: %w", err)
	}
	circuitsByCode := map[string]Circuit{}
	for _, circuit := range circuits {
		circuitsByCode[circuit.Code] = circuit
	}

	for _, circuitCode := range cfg.Circuits {
		wg.Add(1)
		go func(circuitCode string) {
			defer wg.Done()

			series, err := p.GetCircuitLatencies(ctx, GetCircuitLatenciesConfig{
				Circuit:   circuitCode,
				Epochs:    cfg.Epochs,
				Time:      cfg.Time,
				MaxPoints: 1,
				Unit:      cfg.Unit,
			})
			if err != nil {
				p.log.Warn("failed to get circuit latencies", "error", err, "circuit", circuitCode)
				return
			}
			p.log.Debug("Got circuit latencies", "circuit", circuitCode, "series", len(series))
			if len(series) == 0 {
				return
			}
			if len(series) > 1 {
				p.log.Warn("got more than one series for circuit summary", "circuit", circuitCode, "series", len(series))
			}
			measured := series[0]

			// Calculate committed RTT and jitter deltas and change ratios.
			circuit, ok := circuitsByCode[circuitCode]
			if !ok {
				p.log.Warn("circuit not found", "circuit", circuitCode)
				return
			}
			committedRTT := circuit.Link.CommittedRTT
			committedJitter := circuit.Link.CommittedJitter
			switch cfg.Unit {
			case UnitMillisecond:
				committedRTT /= 1000.0
				committedJitter /= 1000.0
			case UnitMicrosecond:
			}
			committedRTTDelta := committedRTT - measured.RTTMean
			committedJitterDelta := committedJitter - measured.JitterAvg
			committedRTTChangeRatio := (committedRTTDelta / committedRTT)
			committedJitterChangeRatio := (committedJitterDelta / committedJitter)

			// Add the circuit summary to the output.
			mu.Lock()
			output = append(output, CircuitSummary{
				Circuit:            circuitCode,
				CircuitLatencyStat: measured,

				CommittedRTT:    committedRTT,
				CommittedJitter: committedJitter,

				CommittedRTTDelta:          committedRTTDelta,
				CommittedJitterDelta:       committedJitterDelta,
				CommittedRTTChangeRatio:    committedRTTChangeRatio,
				CommittedJitterChangeRatio: committedJitterChangeRatio,
			})
			mu.Unlock()
		}(circuitCode)
	}
	wg.Wait()

	sort.Slice(output, func(i, j int) bool {
		if output[i].Circuit == output[j].Circuit {
			return output[i].Timestamp < output[j].Timestamp
		}
		return output[i].Circuit < output[j].Circuit
	})

	return output, nil
}
