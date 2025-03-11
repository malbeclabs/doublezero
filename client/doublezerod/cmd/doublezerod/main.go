package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	solana "github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/runtime"
)

var (
	sockFile             = flag.String("sock-file", "/var/run/doublezerod/doublezerod.sock", "path to doublezerod domain socket")
	enableLatencyProbing = flag.Bool("latency-probing", true, "enable latency probing to doublezero nodes")
	versionFlag          = flag.Bool("version", false, "build version")
	programId            = flag.String("program-id", "", "override smartcontract program id to monitor")
	rpcEndpoint          = flag.String("solana-rpc-endpoint", "", "override solana rpc endpoint url")

	commit  = ""
	version = ""
	date    = ""
)

func main() {

	flag.Parse()

	if *versionFlag {
		fmt.Printf("build: %s\n", commit)
		fmt.Printf("version: %s\n", version)
		fmt.Printf("date: %s\n", date)
		os.Exit(0)
	}

	if *programId != "" {
		_, err := solana.PublicKeyFromBase58(*programId)
		if err != nil {
			log.Fatalf("malformed smartcontract program-id: %v", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runtime.Run(ctx, *sockFile, *enableLatencyProbing, *programId, *rpcEndpoint); err != nil {
		log.Fatalf("runtime error: %v", err)
	}
}
