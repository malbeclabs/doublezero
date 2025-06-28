package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

func main() {
	fmt.Println("Fetching data from the smart contract...")

	programID := solana.MustPublicKeyFromBase58("7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX")
	rpcClient := rpc.New(rpc.LocalNet_RPC)
	client := serviceability.New(rpcClient, programID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Load(ctx); err != nil {
		log.Fatalf("error while loading data: %v", err)
	}

	fmt.Print("Users:\n")
	for _, user := range client.Users {
		fmt.Printf("%+v\n\n", user)
	}
}
