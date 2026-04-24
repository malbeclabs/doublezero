package exporter

import (
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
)

type PartitionKey struct {
	DataProvider    DataProviderName
	SourceMetroPK   solana.PublicKey
	TargetMetroPK   solana.PublicKey
	Epoch           uint64
	SourceMetroCode string
	TargetMetroCode string
}

type Sample struct {
	Timestamp time.Time
	RTT       time.Duration
}

func (k PartitionKey) String() string {
	return fmt.Sprintf("%s-%s-%s-%d", k.DataProvider, k.SourceMetroPK.String(), k.TargetMetroPK.String(), k.Epoch)
}

func (k PartitionKey) CircuitCode() string {
	return fmt.Sprintf("%s → %s", k.SourceMetroCode, k.TargetMetroCode)
}
