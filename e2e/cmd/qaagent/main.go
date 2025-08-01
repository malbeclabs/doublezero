package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/malbeclabs/doublezero/e2e/internal/rpc"
)

var (
	serverAddr = flag.String("server-addr", "localhost:443", "the server address to connect to")
)

func main() {
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	joiner := netutil.NewMulticastListener()
	e, err := rpc.NewQAAgent(*serverAddr, joiner)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	log.Println("Starting QA Agent...")
	if err := e.Start(ctx); err != nil {
		log.Fatalf("failed to start agent: %v", err)
	}
}
