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

	seen := make(map[solana.PublicKey]struct{}, len(recipients))
	for _, r := range recipients {
		seen[r.PubKey] = struct{}{}
	}
	add := func(name string, pk solana.PublicKey) {
		if _, ok := seen[pk]; ok {
			return
		}
		seen[pk] = struct{}{}
		recipients = append(recipients, NewRecipient(name, pk))
	}

	for _, device := range data.Devices {
		devicePK := solana.PublicKeyFromBytes(device.PubKey[:])
		name := fmt.Sprintf("device-%s", devicePK.String())
		add(name, solana.PublicKeyFromBytes(device.MetricsPublisherPubKey[:]))
	}

	for _, contributor := range data.Contributors {
		contributorPK := solana.PublicKeyFromBytes(contributor.PubKey[:])
		name := fmt.Sprintf("contributor-%s", contributorPK.String())
		add(name, solana.PublicKeyFromBytes(contributor.Owner[:]))
	}

	for _, mcastgroup := range data.MulticastGroups {
		mcastgroupPK := solana.PublicKeyFromBytes(mcastgroup.PubKey[:])
		name := fmt.Sprintf("mcastgroup-%s", mcastgroupPK.String())
		add(name, solana.PublicKeyFromBytes(mcastgroup.Owner[:]))
	}

	add("internet-latency-collector", internetLatencyCollectorPK)

	return recipients, nil
}
