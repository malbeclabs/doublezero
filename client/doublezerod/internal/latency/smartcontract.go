package latency

import (
	"context"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
)

type ContractData struct {
	Locations []dzsdk.Location
	Devices   []dzsdk.Device
	Exchanges []dzsdk.Exchange
	Tunnels   []dzsdk.Tunnel
	Users     []dzsdk.User
}

func FetchContractData(ctx context.Context, programId string) (*ContractData, error) {
	options := []dzsdk.Option{}
	if programId != "" {
		options = append(options, dzsdk.WithProgramId(programId))
	}
	client := dzsdk.New(rpc.DevNet_RPC, options...)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Load(ctx); err != nil {
		return nil, err
	}

	// only extract devices for now
	return &ContractData{
		Devices: client.Devices,
	}, nil
}
