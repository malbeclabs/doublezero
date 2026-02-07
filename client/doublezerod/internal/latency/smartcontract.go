package latency

import (
	"context"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

type ContractData struct {
	Locations []serviceability.Location
	Devices   []serviceability.Device
	Exchanges []serviceability.Exchange
	Links     []serviceability.Link
	Users     []serviceability.User
}

func FetchContractData(ctx context.Context, programId string, rpcEndpoint string) (*ContractData, error) {
	programID, err := solana.PublicKeyFromBase58(programId)
	if err != nil {
		return nil, err
	}
	client := serviceability.New(rpc.New(rpcEndpoint), programID)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := client.GetProgramData(ctx)
	if err != nil {
		return nil, err
	}

	// only extract devices for now
	return &ContractData{
		Devices: data.Devices,
	}, nil
}
