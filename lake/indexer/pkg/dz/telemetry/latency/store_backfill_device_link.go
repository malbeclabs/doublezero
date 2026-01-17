package dztelemlatency

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"

	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

// BackfillDeviceLinkLatencyRPC defines the RPC interface needed for device link latency backfill
type BackfillDeviceLinkLatencyRPC interface {
	GetDeviceLatencySamplesTail(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, existingMaxIdx int) (*telemetry.DeviceLatencySamplesHeader, int, []uint32, error)
}

// BackfillDeviceLinkLatencyResult contains the results of a device link latency backfill operation
type BackfillDeviceLinkLatencyResult struct {
	Epoch        uint64
	SampleCount  int
	LinksQueried int
}

// BackfillDeviceLinkLatencyForEpoch fetches and stores device link latency samples for a single epoch.
// It fetches all samples (existingMaxIdx = -1) and relies on ReplacingMergeTree for deduplication.
func (s *Store) BackfillDeviceLinkLatencyForEpoch(
	ctx context.Context,
	rpc BackfillDeviceLinkLatencyRPC,
	devices []dzsvc.Device,
	links []dzsvc.Link,
	epoch uint64,
	maxConcurrency int,
) (*BackfillDeviceLinkLatencyResult, error) {
	// Build device map for lookup
	devicesByPK := make(map[string]dzsvc.Device)
	for _, d := range devices {
		devicesByPK[d.PK] = d
	}

	var allSamples []DeviceLinkLatencySample
	var samplesMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrency)
	linksQueried := 0

	for _, link := range links {
		deviceA, okA := devicesByPK[link.SideAPK]
		deviceZ, okZ := devicesByPK[link.SideZPK]
		if !okA || !okZ {
			continue
		}

		// Process both directions: A -> Z and Z -> A
		for _, direction := range []struct {
			originPK, targetPK string
		}{
			{deviceA.PK, deviceZ.PK},
			{deviceZ.PK, deviceA.PK},
		} {
			originPK, err := solana.PublicKeyFromBase58(direction.originPK)
			if err != nil {
				continue
			}
			targetPK, err := solana.PublicKeyFromBase58(direction.targetPK)
			if err != nil {
				continue
			}
			linkPK, err := solana.PublicKeyFromBase58(link.PK)
			if err != nil {
				continue
			}

			linksQueried++
			wg.Add(1)
			sem <- struct{}{}
			go func(originDevicePK, targetDevicePK, linkPKStr string, originPK, targetPK, linkPK solana.PublicKey) {
				defer wg.Done()
				defer func() { <-sem }()

				// For backfill, we fetch all samples (existingMaxIdx = -1)
				// The ReplacingMergeTree will handle deduplication
				hdr, startIdx, tail, err := rpc.GetDeviceLatencySamplesTail(ctx, originPK, targetPK, linkPK, epoch, -1)
				if err != nil {
					if errors.Is(err, telemetry.ErrAccountNotFound) {
						return
					}
					s.log.Debug("failed to get device latency samples", "error", err, "epoch", epoch, "link", linkPKStr)
					return
				}
				if hdr == nil || len(tail) == 0 {
					return
				}

				linkSamples := make([]DeviceLinkLatencySample, 0, len(tail))
				step := hdr.SamplingIntervalMicroseconds
				baseTs := hdr.StartTimestampMicroseconds + uint64(startIdx)*step
				for j, rtt := range tail {
					i := startIdx + j
					ts := baseTs + uint64(j)*step
					sampleTime := time.Unix(int64(ts)/1_000_000, (int64(ts)%1_000_000)*1000)
					linkSamples = append(linkSamples, DeviceLinkLatencySample{
						OriginDevicePK:  originDevicePK,
						TargetDevicePK:  targetDevicePK,
						LinkPK:          linkPKStr,
						Epoch:           epoch,
						SampleIndex:     i,
						Time:            sampleTime,
						RTTMicroseconds: rtt,
					})
				}

				if len(linkSamples) > 0 {
					samplesMu.Lock()
					allSamples = append(allSamples, linkSamples...)
					samplesMu.Unlock()
				}
			}(direction.originPK, direction.targetPK, link.PK, originPK, targetPK, linkPK)
		}
	}

	wg.Wait()

	if len(allSamples) > 0 {
		if err := s.AppendDeviceLinkLatencySamples(ctx, allSamples); err != nil {
			return nil, err
		}
	}

	return &BackfillDeviceLinkLatencyResult{
		Epoch:        epoch,
		SampleCount:  len(allSamples),
		LinksQueried: linksQueried,
	}, nil
}
