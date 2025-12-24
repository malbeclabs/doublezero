package dztelem

import (
	"context"
	"errors"
	"fmt"
	"sync"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	dzsvc "github.com/malbeclabs/doublezero/tools/mcp/internal/dz/serviceability"
)

type DeviceLinkCircuit struct {
	Code            string
	OriginDevicePK  string
	TargetDevicePK  string
	LinkPK          string
	LinkCode        string
	LinkType        string
	ContributorCode string
	CommittedRTT    float64
	CommittedJitter float64
}

func ComputeDeviceLinkCircuits(devices []dzsvc.Device, links []dzsvc.Link, contributors []dzsvc.Contributor) []DeviceLinkCircuit {
	devicesByPK := make(map[string]dzsvc.Device)
	for _, d := range devices {
		devicesByPK[d.PK] = d
	}

	contributorsByPK := make(map[string]dzsvc.Contributor)
	for _, c := range contributors {
		contributorsByPK[c.PK] = c
	}

	circuits := make([]DeviceLinkCircuit, 0, 2*len(links))
	for _, link := range links {
		deviceA, okA := devicesByPK[link.SideAPK]
		deviceZ, okZ := devicesByPK[link.SideZPK]
		contributor, okC := contributorsByPK[link.ContributorPK]

		if !okA || !okZ || !okC {
			continue
		}

		// Convert delay and jitter from nanoseconds to microseconds
		committedRTT := float64(link.DelayNs) / 1000.0
		committedJitter := float64(link.JitterNs) / 1000.0

		// Forward circuit: A -> Z
		forwardCode := fmt.Sprintf("%s → %s (%s)", deviceA.Code, deviceZ.Code, link.PK[len(link.PK)-7:])
		circuits = append(circuits, DeviceLinkCircuit{
			Code:            forwardCode,
			OriginDevicePK:  deviceA.PK,
			TargetDevicePK:  deviceZ.PK,
			LinkPK:          link.PK,
			LinkCode:        link.Code,
			LinkType:        link.LinkType,
			ContributorCode: contributor.Code,
			CommittedRTT:    committedRTT,
			CommittedJitter: committedJitter,
		})

		// Reverse circuit: Z -> A
		reverseCode := fmt.Sprintf("%s → %s (%s)", deviceZ.Code, deviceA.Code, link.PK[len(link.PK)-7:])
		circuits = append(circuits, DeviceLinkCircuit{
			Code:            reverseCode,
			OriginDevicePK:  deviceZ.PK,
			TargetDevicePK:  deviceA.PK,
			LinkPK:          link.PK,
			LinkCode:        link.Code,
			LinkType:        link.LinkType,
			ContributorCode: contributor.Code,
			CommittedRTT:    committedRTT,
			CommittedJitter: committedJitter,
		})
	}

	return circuits
}

type DeviceLinkLatencySample struct {
	CircuitCode           string
	Epoch                 uint64
	SampleIndex           int
	TimestampMicroseconds uint64
	RTTMicroseconds       uint32
}

func (v *View) refreshDeviceLinkTelemetrySamples(ctx context.Context, circuits []DeviceLinkCircuit) error {
	v.log.Debug("telemetry/device-link: starting sample refresh", "circuits", len(circuits))

	// Get current epoch
	epochInfo, err := v.cfg.EpochRPC.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := epochInfo.Epoch
	v.log.Debug("telemetry/device-link: current epoch", "epoch", currentEpoch)

	// Fetch samples for current epoch and previous epoch
	epochsToFetch := []uint64{currentEpoch}
	if currentEpoch > 0 {
		epochsToFetch = append(epochsToFetch, currentEpoch-1)
	}
	v.log.Debug("telemetry/device-link: fetching epochs", "epochs", epochsToFetch, "max_concurrency", v.cfg.MaxConcurrency)

	// Get existing max sample_index for each circuit_code+epoch to determine what's new
	existingMaxIndices, err := v.store.GetExistingMaxSampleIndices()
	if err != nil {
		v.log.Warn("telemetry/device-link: failed to get existing max indices, will insert all samples", "error", err)
		existingMaxIndices = make(map[string]int) // Empty map means no existing data
	} else {
		v.log.Debug("telemetry/device-link: found existing max indices", "count", len(existingMaxIndices))
	}

	var allSamples []DeviceLinkLatencySample
	var samplesMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, v.cfg.MaxConcurrency)
	circuitsProcessed := 0
	circuitsWithSamples := 0
	var circuitsWithSamplesMu sync.Mutex

	for _, circuit := range circuits {
		// Check for context cancellation before starting new goroutines
		select {
		case <-ctx.Done():
			v.log.Debug("telemetry/device-link: context cancelled, stopping circuit processing")
			goto done
		default:
		}

		circuitsProcessed++
		originPK, err := solana.PublicKeyFromBase58(circuit.OriginDevicePK)
		if err != nil {
			v.log.Debug("telemetry/device-link: invalid origin device PK", "circuit", circuit.Code, "error", err)
			continue
		}
		targetPK, err := solana.PublicKeyFromBase58(circuit.TargetDevicePK)
		if err != nil {
			v.log.Debug("telemetry/device-link: invalid target device PK", "circuit", circuit.Code, "error", err)
			continue
		}
		linkPK, err := solana.PublicKeyFromBase58(circuit.LinkPK)
		if err != nil {
			v.log.Debug("telemetry/device-link: invalid link PK", "circuit", circuit.Code, "error", err)
			continue
		}

		wg.Add(1)
		// Try to acquire semaphore with context cancellation support
		select {
		case <-ctx.Done():
			wg.Done()
			goto done
		case sem <- struct{}{}:
			go func(circuit DeviceLinkCircuit, originPK, targetPK, linkPK solana.PublicKey) {
				defer wg.Done()
				defer func() { <-sem }() // Release semaphore

				circuitHasSamples := false
				var circuitSamples []DeviceLinkLatencySample

				for _, epoch := range epochsToFetch {
					// Check for context cancellation before each RPC call
					select {
					case <-ctx.Done():
						v.log.Debug("telemetry/device-link: context cancelled during fetch", "circuit", circuit.Code)
						return
					default:
					}

					samples, err := v.cfg.TelemetryRPC.GetDeviceLatencySamples(ctx, originPK, targetPK, linkPK, epoch)
					if err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							return
						}
						if errors.Is(err, telemetry.ErrAccountNotFound) {
							v.log.Debug("telemetry/device-link: no samples found", "circuit", circuit.Code, "epoch", epoch)
							continue
						}
						v.log.Debug("telemetry/device-link: failed to get latency samples", "circuit", circuit.Code, "epoch", epoch, "error", err)
						continue
					}

					circuitHasSamples = true
					sampleCount := len(samples.Samples)
					nextSampleIndex := samples.NextSampleIndex
					v.log.Debug("telemetry/device-link: fetched samples", "circuit", circuit.Code, "epoch", epoch, "samples", sampleCount, "next_sample_index", nextSampleIndex)

					// Check what's already in the database for this circuit+epoch
					key := fmt.Sprintf("%s:%d", circuit.Code, epoch)
					existingMaxIdx := -1
					if maxIdx, ok := existingMaxIndices[key]; ok {
						existingMaxIdx = maxIdx
					}

					// Convert samples to our format - only include new samples (sample_index > existingMaxIdx)
					// Samples are only appended, so we can use NextSampleIndex to determine what's new
					converted := convertDeviceLatencySamples(samples, circuit.Code, epoch)
					newSamples := 0
					for _, sample := range converted {
						if sample.SampleIndex > existingMaxIdx {
							circuitSamples = append(circuitSamples, sample)
							newSamples++
						}
					}
					if newSamples > 0 {
						v.log.Debug("telemetry/device-link: found new samples", "circuit", circuit.Code, "epoch", epoch, "existing_max_idx", existingMaxIdx, "new_samples", newSamples, "total_samples", len(converted))
					}
				}

				if circuitHasSamples {
					circuitsWithSamplesMu.Lock()
					circuitsWithSamples++
					circuitsWithSamplesMu.Unlock()
				}

				// Append samples to shared slice
				if len(circuitSamples) > 0 {
					samplesMu.Lock()
					allSamples = append(allSamples, circuitSamples...)
					samplesMu.Unlock()
				}
			}(circuit, originPK, targetPK, linkPK)
		}
	}

done:
	wg.Wait()

	v.log.Debug("telemetry/device-link: processed circuits", "total", circuitsProcessed, "with_samples", circuitsWithSamples, "total_samples", len(allSamples))

	// Append new samples to table (instead of replacing)
	if len(allSamples) > 0 {
		v.log.Debug("telemetry/device-link: appending new latency samples", "new_samples", len(allSamples))
		if err := v.store.AppendDeviceLinkLatencySamples(allSamples); err != nil {
			v.log.Error("telemetry/device-link: failed to append latency samples", "error", err, "total_samples", len(allSamples))
			return fmt.Errorf("failed to append latency samples: %w", err)
		}
		v.log.Debug("telemetry/device-link: sample refresh completed", "samples_inserted", len(allSamples))
	} else {
		v.log.Debug("telemetry/device-link: no new samples to insert")
	}
	return nil
}

func convertDeviceLatencySamples(samples *telemetry.DeviceLatencySamples, circuitCode string, epoch uint64) []DeviceLinkLatencySample {
	result := make([]DeviceLinkLatencySample, len(samples.Samples))
	for i, rtt := range samples.Samples {
		timestamp := samples.StartTimestampMicroseconds + uint64(i)*samples.SamplingIntervalMicroseconds
		result[i] = DeviceLinkLatencySample{
			CircuitCode:           circuitCode,
			Epoch:                 epoch,
			SampleIndex:           i,
			TimestampMicroseconds: timestamp,
			RTTMicroseconds:       rtt,
		}
	}
	return result
}
