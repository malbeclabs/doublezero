package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
)

func main() {

	fmt.Println("Fetching data from the smart contract...")

	c := dzsdk.New(rpc.DevNet_RPC, dzsdk.WithProgramId(dzsdk.PROGRAM_ID_DEVNET))
	// c := dzsdk.New(rpc.DevNet_RPC, dzsdk.WithProgramId(dzsdk.PROGRAM_ID_TESTNET))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Load(ctx); err != nil {
		log.Fatalf("error while loading data: %v", err)
	}

	fmt.Print("Users:\n")
	for _, user := range c.Users {
		fmt.Printf("%+v\n\n", user)
	}
}
