package data

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

const maxAgentVersionStaleness = 24 * time.Hour

func (p *provider) GetAgentVersions(ctx context.Context) ([]DeviceAgentVersion, error) {
	circuits, err := p.GetCircuits(ctx)
	if err != nil {
		return nil, err
	}

	currentEpoch, err := p.cfg.EpochFinder.ApproximateAtTime(ctx, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	// Group circuits by origin device, keeping one representative circuit per device.
	type deviceCircuit struct {
		devicePK   solana.PublicKey
		deviceCode string
		circuit    Circuit
	}
	seen := map[solana.PublicKey]struct{}{}
	var devices []deviceCircuit
	for _, c := range circuits {
		if _, ok := seen[c.OriginDevice.PK]; ok {
			continue
		}
		seen[c.OriginDevice.PK] = struct{}{}
		devices = append(devices, deviceCircuit{
			devicePK:   c.OriginDevice.PK,
			deviceCode: c.OriginDevice.Code,
			circuit:    c,
		})
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var results []DeviceAgentVersion
	now := time.Now().UTC()

	sem := make(chan struct{}, defaultGetCircuitLatenciesPoolSize)
	for _, dev := range devices {
		wg.Add(1)
		sem <- struct{}{}
		go func(dev deviceCircuit) {
			defer func() { <-sem; wg.Done() }()

			// Try current epoch first, then the previous epoch.
			var hdr *telemetry.DeviceLatencySamplesHeader
			for _, ep := range []uint64{currentEpoch, currentEpoch - 1} {
				h, err := p.cfg.TelemetryClient.GetDeviceLatencySamplesHeader(
					ctx,
					dev.circuit.OriginDevice.PK,
					dev.circuit.TargetDevice.PK,
					dev.circuit.Link.PK,
					ep,
				)
				if err != nil {
					if errors.Is(err, telemetry.ErrAccountNotFound) {
						continue
					}
					p.log.Warn("failed to get samples header", "device", dev.deviceCode, "epoch", ep, "error", err)
					continue
				}
				if h.NextSampleIndex == 0 {
					continue
				}
				hdr = h
				break
			}

			if hdr == nil || hdr.NextSampleIndex == 0 {
				return
			}

			ts := lastSampleTime(hdr)
			if now.Sub(ts) > maxAgentVersionStaleness {
				return
			}

			version := strings.TrimRight(string(hdr.AgentVersion[:]), "\x00")
			commit := strings.TrimRight(string(hdr.AgentCommit[:]), "\x00")

			mu.Lock()
			results = append(results, DeviceAgentVersion{
				DevicePK:   dev.devicePK.String(),
				DeviceCode: dev.deviceCode,
				Version:    version,
				Commit:     commit,
				Timestamp:  ts.Format(time.RFC3339),
			})
			mu.Unlock()
		}(dev)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		return results[i].DeviceCode < results[j].DeviceCode
	})

	return results, nil
}

func lastSampleTime(hdr *telemetry.DeviceLatencySamplesHeader) time.Time {
	if hdr.NextSampleIndex == 0 {
		return time.Time{}
	}
	tsMicros := hdr.StartTimestampMicroseconds +
		uint64(hdr.NextSampleIndex-1)*hdr.SamplingIntervalMicroseconds
	secs := int64(tsMicros / 1_000_000)
	nanos := int64(tsMicros%1_000_000) * 1000
	return time.Unix(secs, nanos)
}
