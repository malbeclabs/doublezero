package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/openconfig/grpctunnel/bidi"
	"github.com/openconfig/grpctunnel/tunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	tpb "github.com/openconfig/grpctunnel/proto/tunnel"
)

var (
	target        = flag.String("target", "dzd01", "Target name to use when registering with the tunnel server")
	targetType    = flag.String("target-type", "GNMI_GNOI", "The target type to register")
	localDialAddr = flag.String("local-dial-addr", "/var/run/gnmiServer.sock", "The local dial address to connect to")
	tunServerAddr = flag.String("tunnel-server-addr", "localhost:10000", "The tunnel server address to connect to")
)

func main() {
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		bo := backoff.NewExponentialBackOff()
		bo.MaxElapsedTime = 0
		bo.InitialInterval = 1 * time.Second
		bo.MaxInterval = 1 * time.Minute
		bo.RandomizationFactor = 0.5
		var err error
		for {
			if err = run(ctx, logger); err != nil {
				logger.Error("tunnel client error", "error", err)
				wait := bo.NextBackOff()
				logger.Info("reconnecting", "in", wait)
				time.Sleep(wait)
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	<-ctx.Done()
}

func run(ctx context.Context, logger *slog.Logger) error {
	clientConn, err := grpc.NewClient(*tunServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("grpc dial error: %w", err)
	}
	defer clientConn.Close()

	logger.Info("tunnel client connected to", "address", *tunServerAddr)

	registerHandler := func(t tunnel.Target) error {
		if t.ID == *target {
			return nil
		}
		return fmt.Errorf("could not register handler for target: %s", t.ID)
	}

	handler := func(t tunnel.Target, i io.ReadWriteCloser) error {
		var dialAddr string
		if t.ID == *target && t.Type == *targetType {
			dialAddr = *localDialAddr
		}
		if len(dialAddr) == 0 {
			return fmt.Errorf("no local port found for target: %s", t.ID)
		}

		conn, err := net.Dial("unix", dialAddr)
		if err != nil {
			return fmt.Errorf("failed to dial %s: %v", dialAddr, err)
		}

		logger.Info("new session established for target", "target", t)
		if err = bidi.Copy(i, conn); err != nil {
			return fmt.Errorf("error while copying between connections: %v", err)
		}
		return nil
	}

	ts := make(map[tunnel.Target]struct{})
	t := tunnel.Target{ID: *target, Type: *targetType}
	ts[t] = struct{}{}

	client, err := tunnel.NewClient(tpb.NewTunnelClient(clientConn), tunnel.ClientConfig{
		RegisterHandler: registerHandler,
		Handler:         handler,
	}, ts)

	if err != nil {
		return fmt.Errorf("failed to create tunnel client: %w", err)
	}

	if err := client.Register(ctx); err != nil {
		return fmt.Errorf("failed to register tunnel client: %w", err)
	}
	logger.Info("tunnel client registered to", "address", *tunServerAddr)

	client.Start(ctx)
	return client.Error()
}
