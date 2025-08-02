package telemetry

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
)

type PartitionKey struct {
	OriginDevicePK solana.PublicKey
	TargetDevicePK solana.PublicKey
	LinkPK         solana.PublicKey
	Epoch          uint64
}

func (k PartitionKey) String() string {
	return fmt.Sprintf("%s-%s-%s-%d", k.OriginDevicePK.String(), k.TargetDevicePK.String(), k.LinkPK.String(), k.Epoch)
}
