package data

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/circuits"
)

type Device struct {
	PK       solana.PublicKey `json:"pk"`
	Code     string           `json:"code"`
	Location Location         `json:"location"`
}

type Link struct {
	PK              solana.PublicKey `json:"pk"`
	Code            string           `json:"code"`
	LinkType        string           `json:"link_type"`
	ContributorCode string           `json:"contributor_code"`

	// Committed RTT and jitter are in microseconds.
	CommittedRTT    float64 `json:"committed_rtt"`
	CommittedJitter float64 `json:"committed_jitter"`
}

type Location struct {
	PK        solana.PublicKey `json:"pk"`
	Name      string           `json:"name"`
	Country   string           `json:"country"`
	Latitude  float64          `json:"latitude"`
	Longitude float64          `json:"longitude"`
}

type Circuit struct {
	Code         string `json:"code"`
	OriginDevice Device `json:"origin_device"`
	TargetDevice Device `json:"target_device"`
	Link         Link   `json:"link"`
}

func (p *provider) GetCircuits(ctx context.Context) ([]Circuit, error) {
	cached := p.GetCachedCircuits(ctx)
	if cached != nil {
		return cached, nil
	}

	deviceLinkCircuits, err := circuits.GetDeviceLinkCircuits(ctx, p.cfg.Logger, p.cfg.ServiceabilityClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get device link circuits: %w", err)
	}

	circuits := make([]Circuit, 0, len(deviceLinkCircuits))
	for _, circuit := range deviceLinkCircuits {
		circuits = append(circuits, Circuit{
			Code: circuit.Code,
			OriginDevice: Device{
				PK:   circuit.OriginDevice.PubKey,
				Code: circuit.OriginDevice.Code,
				Location: Location{
					PK: circuit.OriginDevice.LocationPubKey,
				},
			},
			TargetDevice: Device{
				PK:   circuit.TargetDevice.PubKey,
				Code: circuit.TargetDevice.Code,
				Location: Location{
					PK: circuit.TargetDevice.LocationPubKey,
				},
			},
			Link: Link{
				PK:              circuit.Link.PubKey,
				Code:            circuit.Link.Code,
				LinkType:        circuit.Link.LinkType.String(),
				ContributorCode: circuit.Contributor.Code,
				CommittedRTT:    float64(circuit.Link.DelayNs) / 1000.0,
				CommittedJitter: float64(circuit.Link.JitterNs) / 1000.0,
			},
		})
	}

	p.SetCachedCircuits(ctx, circuits)

	return circuits, nil
}
