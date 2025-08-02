//go:build linux

package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path"
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

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				s := a.Value.Any().(*slog.Source)
				s.File = path.Base(s.File)
			}
			return a
		},
	}))
	joiner := netutil.NewMulticastListener()
	e, err := rpc.NewQAAgent(log, *serverAddr, joiner)
	if err != nil {
		log.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	log.Info("Starting QA Agent...")
	if err := e.Start(ctx); err != nil {
		log.Error("failed to start agent", "error", err)
		os.Exit(1)
	}
}
