package circuits

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type ServiceabilityClient interface {
	GetProgramData(context.Context) (*serviceability.ProgramData, error)
}

type DeviceLinkCircuit struct {
	Code         string
	OriginDevice serviceability.Device
	TargetDevice serviceability.Device
	Link         serviceability.Link
}

func GetDeviceLinkCircuits(ctx context.Context, log *slog.Logger, serviceabilityClient ServiceabilityClient) ([]DeviceLinkCircuit, error) {
	data, err := serviceabilityClient.GetProgramData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load serviceability data: %w", err)
	}

	circuits := make([]DeviceLinkCircuit, 0, 2*len(data.Links))

	devicesByPK := map[string]serviceability.Device{}
	for _, device := range data.Devices {
		pk := solana.PublicKeyFromBytes(device.PubKey[:])
		devicesByPK[pk.String()] = device
	}

	for _, link := range data.Links {
		deviceAPK := solana.PublicKeyFromBytes(link.SideAPubKey[:])
		deviceZPK := solana.PublicKeyFromBytes(link.SideZPubKey[:])
		linkPK := solana.PublicKeyFromBytes(link.PubKey[:])

		deviceA, ok := devicesByPK[deviceAPK.String()]
		if !ok {
			log.Warn("device A not found, skipping link", "link_code", link.Code, "device_a_pk", deviceAPK.String())
			continue
		}
		deviceZ, ok := devicesByPK[deviceZPK.String()]
		if !ok {
			log.Warn("device Z not found, skipping link", "link_code", link.Code, "device_z_pk", deviceZPK.String())
			continue
		}

		// Forward circuit
		forwardKey := deviceLinkCircuitKey(deviceA.Code, deviceZ.Code, linkPK)
		circuits = append(circuits, DeviceLinkCircuit{
			Code:         forwardKey,
			OriginDevice: deviceA,
			TargetDevice: deviceZ,
			Link:         link,
		})

		// Reverse circuit
		reverseKey := deviceLinkCircuitKey(deviceZ.Code, deviceA.Code, linkPK)
		circuits = append(circuits, DeviceLinkCircuit{
			Code:         reverseKey,
			OriginDevice: deviceZ,
			TargetDevice: deviceA,
			Link:         link,
		})
	}

	sort.Slice(circuits, func(i, j int) bool {
		return circuits[i].Code < circuits[j].Code
	})

	return circuits, nil
}

func deviceLinkCircuitKey(origin, target string, linkPK solana.PublicKey) string {
	linkPKStr := linkPK.String()
	start := 0
	if len(linkPKStr) > 7 {
		start = len(linkPKStr) - 7
	}
	shortLinkPK := linkPKStr[start:]
	return fmt.Sprintf("%s → %s (%s)", origin, target, shortLinkPK)
}
