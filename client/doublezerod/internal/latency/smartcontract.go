package latency

import (
	"context"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type ContractData struct {
	Locations []serviceability.Location
	Devices   []serviceability.Device
	Exchanges []serviceability.Exchange
	Links     []serviceability.Link
	Users     []serviceability.User
}

func FetchContractData(ctx context.Context, programId string, rpcEndpoint string) (*ContractData, error) {
	if rpcEndpoint == "" {
		rpcEndpoint = dzsdk.DZ_LEDGER_RPC_URL
	}
	programID, err := solana.PublicKeyFromBase58(programId)
	if err != nil {
		return nil, err
	}
	client := serviceability.New(rpc.New(rpcEndpoint), programID)
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
