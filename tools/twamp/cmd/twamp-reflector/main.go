package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

func main() {
	if len(os.Args) < 1 || len(os.Args) > 2 {
		fmt.Fprintf(os.Stderr, "Usage: twamp-reflector [host:port]\n")
		os.Exit(1)
	}

	listenAddr := "0.0.0.0:0"
	if len(os.Args) == 2 {
		listenAddr = os.Args[1]
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	reflector, err := twamplight.NewReflector(log, listenAddr, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create reflector: %v\n", err)
		os.Exit(1)
	}
	defer reflector.Close()

	fmt.Printf("Listening on %s\n", reflector.LocalAddr())

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := reflector.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
