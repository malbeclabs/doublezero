package rewards

import (
	"context"
	"log"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
)

type Client struct {
	rpc       RPCClient
	programID solana.PublicKey
}

func New(rpc RPCClient, programID solana.PublicKey) *Client {
	return &Client{rpc: rpc, programID: programID}
}

func (c *Client) GetTelemetryStats(ctx context.Context) (DZDTelemetryStats, error) {
	data, err := c.rpc.GetProgramAccounts(ctx, c.programID)
	if err != nil {
		return nil, err
	}
	stats := make(DZDTelemetryStats)
	for _, element := range data {
		record := &RecordData{}
		data := element.Account.Data.GetBinary()
		if len(data) < 33 {
			continue
		}
		dec := bin.NewBorshDecoder(data)
		if err := dec.Decode(record); err != nil {
			//log.Printf("Failed to decode record for account %s: %v", element.Pubkey.String(), err)
			continue
		}
		for _, v := range record.Stat {
			log.Printf("%s: %f", v.Circuit, v.RttMeanUs)
		}
		// if err := stats.Deserialize(data[31:]); err != nil {
		// 	log.Printf("Failed to deserialize telemetry stat for account %s: %v", element.Pubkey.String(), err)
		// 	continue
		// }
		//log.Printf("Successfully deserialized telemetry stat for account %s: %+v", element.Pubkey.String(), stats[element.Pubkey.String()])
	}
	return stats, nil
}

type RecordData struct {
	Version   uint8
	Authority solana.PublicKey
	Stat      map[string]DZDTelemetryStat
}

type DZDTelemetryStats map[string]*DZDTelemetryStat

func (s *DZDTelemetryStats) Deserialize(data []byte) error {
	dec := bin.NewBorshDecoder(data)
	if err := dec.Decode(s); err != nil {
		return err
	}
	return nil
}

type DZDTelemetryStat struct {
	Circuit        string
	LinkPK         solana.PublicKey
	OriginDevicePK solana.PublicKey
	TargetDevicePK solana.PublicKey
	RttMeanUs      float64
	RttMedianUs    float64
	RttMinUs       float64
	RttMaxUs       float64
	RttP90Us       float64
	RttP95Us       float64
	RttP99Us       float64
	RttStdDevUs    float64
	AvgJitterUs    float64
	JitterEwmaUs   float64
	MaxJitterUs    float64
	PacketLossPct  float64
	LossCount      uint64
	SuccessCount   uint64
	TotalSamples   uint64
}

func (s *DZDTelemetryStat) Deserialize(data []byte) error {
	dec := bin.NewBorshDecoder(data)
	if err := dec.Decode(s); err != nil {
		return err
	}
	return nil
}
