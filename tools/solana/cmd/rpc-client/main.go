package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
)

func main() {
	solanaNetworkConfig, err := config.SolanaNetworkConfigForEnv(config.SolanaEnvMainnetBeta)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	solanaRPC := solanarpc.New(solanaNetworkConfig.RPCURL)
	nodesFromRPC, err := solanaRPC.GetClusterNodes(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, node := range nodesFromRPC {
		if node.TPUQUIC == nil {
			continue
		}
		fmt.Println(node.Pubkey.String(), *node.Gossip, *node.TPUQUIC)
	}
}
