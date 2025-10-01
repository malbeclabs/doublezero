//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"

	_ "net/http/pprof"

	"github.com/malbeclabs/doublezero/e2e/internal/rpc"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	serverAddr  = flag.String("server-addr", "localhost:443", "the server address to connect to")
	showVersion = flag.Bool("version", false, "show version information and exit")
	metricsAddr = flag.String("metrics-addr", "127.0.0.1:2112", "the address to expose metrics")
	enablePprof = flag.Bool("enable-pprof", false, "enable pprof server")

	// set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

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

	rpc.BuildInfo.WithLabelValues(version, commit, date).Set(1)

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(*metricsAddr, mux) //nolint
	}()

	if *enablePprof {
		go func() {
			err := http.ListenAndServe("localhost:6060", nil)
			if err != nil {
				log.Error("failed to start pprof server", "error", err)
			}
		}()
	}

	e, err := rpc.NewQAAgent(log, *serverAddr)
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
