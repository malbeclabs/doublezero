package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

func main() {
	fmt.Println("Fetching data from the smart contract...")

	programID := solana.MustPublicKeyFromBase58("7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX")
	rpcClient := rpc.New(rpc.LocalNet_RPC)
	client := serviceability.New(rpcClient, programID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := client.GetProgramData(ctx)
	if err != nil {
		log.Fatalf("error while loading data: %v", err)
	}

	fmt.Print("Users:\n")
	for _, user := range data.Users {
		fmt.Printf("%+v\n\n", user)
	}
}
