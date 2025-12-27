package dztelemlatency

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	dzsvc "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/dz/serviceability"
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
	// Get current epoch
	epochInfo, err := v.cfg.EpochRPC.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := epochInfo.Epoch

	// Fetch samples for current epoch and previous epoch
	epochsToFetch := []uint64{currentEpoch}
	if currentEpoch > 0 {
		epochsToFetch = append(epochsToFetch, currentEpoch-1)
	}

	// Get existing max sample_index for each circuit_code+epoch to determine what's new
	existingMaxIndices, err := v.store.GetExistingMaxSampleIndices()
	if err != nil {
		v.log.Warn("telemetry/device-link: failed to get existing max indices, will insert all samples", "error", err)
		existingMaxIndices = make(map[string]int) // Empty map means no existing data
	}

	var allSamples []DeviceLinkLatencySample
	var samplesMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, v.cfg.MaxConcurrency)
	circuitsProcessed := 0

	for _, circuit := range circuits {
		// Check for context cancellation before starting new goroutines
		select {
		case <-ctx.Done():
			goto done
		default:
		}

		circuitsProcessed++
		originPK, err := solana.PublicKeyFromBase58(circuit.OriginDevicePK)
		if err != nil {
			continue
		}
		targetPK, err := solana.PublicKeyFromBase58(circuit.TargetDevicePK)
		if err != nil {
			continue
		}
		linkPK, err := solana.PublicKeyFromBase58(circuit.LinkPK)
		if err != nil {
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

				circuitSamples := make([]DeviceLinkLatencySample, 0, 128)

				for _, epoch := range epochsToFetch {
					// Check for context cancellation before each RPC call
					select {
					case <-ctx.Done():
						return
					default:
					}

					// Check what's already in the database for this circuit+epoch
					key := circuit.Code + ":" + strconv.FormatUint(epoch, 10)
					existingMaxIdx := -1
					if maxIdx, ok := existingMaxIndices[key]; ok {
						existingMaxIdx = maxIdx
					}

					hdr, startIdx, tail, err := v.cfg.TelemetryRPC.GetDeviceLatencySamplesTail(ctx, originPK, targetPK, linkPK, epoch, existingMaxIdx)
					if err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							return
						}
						if errors.Is(err, telemetry.ErrAccountNotFound) {
							continue
						}
						continue
					}
					if hdr == nil {
						continue
					}

					if len(tail) > 0 {
						step := hdr.SamplingIntervalMicroseconds
						baseTs := hdr.StartTimestampMicroseconds + uint64(startIdx)*step
						for j, rtt := range tail {
							i := startIdx + j
							ts := baseTs + uint64(j)*step
							circuitSamples = append(circuitSamples, DeviceLinkLatencySample{
								CircuitCode:           circuit.Code,
								Epoch:                 epoch,
								SampleIndex:           i,
								TimestampMicroseconds: ts,
								RTTMicroseconds:       rtt,
							})
						}
					}
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

	// Append new samples to table (instead of replacing)
	if len(allSamples) > 0 {
		if err := v.store.AppendDeviceLinkLatencySamples(ctx, allSamples); err != nil {
			return fmt.Errorf("failed to append latency samples: %w", err)
		}
		v.log.Debug("telemetry/device-link: sample refresh completed", "circuits", circuitsProcessed, "samples", len(allSamples))
	}
	return nil
}
