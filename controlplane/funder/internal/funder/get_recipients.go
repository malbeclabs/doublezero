package funder

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

func GetRecipients(
	ctx context.Context,
	serviceabilityClient serviceability.ProgramDataProvider,
	recipients []Recipient,
	internetLatencyCollectorPK solana.PublicKey,
) ([]Recipient, error) {
	data, err := serviceabilityClient.GetProgramData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load serviceability state: %w", err)
	}

	for _, device := range data.Devices {
		devicePK := solana.PublicKeyFromBytes(device.PubKey[:])
		name := fmt.Sprintf("device-%s", devicePK.String())
		recipients = append(recipients, NewRecipient(name, solana.PublicKeyFromBytes(device.MetricsPublisherPubKey[:])))
	}

	for _, mcastgroup := range data.MulticastGroups {
		mcastgroupPK := solana.PublicKeyFromBytes(mcastgroup.PubKey[:])
		name := fmt.Sprintf("mcastgroup-%s", mcastgroupPK.String())
		recipients = append(recipients, NewRecipient(name, solana.PublicKeyFromBytes(mcastgroup.Owner[:])))
	}

	recipients = append(recipients, NewRecipient("internet-latency-collector", internetLatencyCollectorPK))

	return recipients, nil
}
