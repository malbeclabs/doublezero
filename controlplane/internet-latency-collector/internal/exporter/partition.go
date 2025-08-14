package exporter

import (
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
)

type PartitionKey struct {
	DataProvider     DataProviderName
	SourceExchangePK solana.PublicKey
	TargetExchangePK solana.PublicKey
	Epoch            uint64
}

type Sample struct {
	Timestamp time.Time
	RTT       time.Duration
}

func (k PartitionKey) String() string {
	return fmt.Sprintf("%s-%s-%s-%d", k.DataProvider, k.SourceExchangePK.String(), k.TargetExchangePK.String(), k.Epoch)
}
