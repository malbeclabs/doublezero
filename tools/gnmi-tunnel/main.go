package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	tpb "github.com/openconfig/grpctunnel/proto/tunnel"
	"github.com/openconfig/grpctunnel/tunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	target        = flag.String("target", "dzd01", "Target name to use when registering with the tunnel server")
	targetType    = flag.String("target-type", "GNMI_GNOI", "The target type to register")
	localDialAddr = flag.String("local-dial-addr", "/var/run/gnmiServer.sock", "The local dial address to connect to")
	tunServerAddr = flag.String("tunnel-server-addr", "localhost:10000", "The tunnel server address to connect to")

	// TLS options for connecting to the tunnel server.
	useTLS        = flag.Bool("tls", true, "Use TLS when connecting to the tunnel server")
	tlsServerName = flag.String("tls-server-name", "", "TLS server name (SNI/hostname verification). Recommended when connecting by IP.")
	tlsCA         = flag.String("tls-ca", "", "Path to CA PEM to trust for the tunnel server (optional if publicly trusted)")
	tlsCert       = flag.String("tls-cert", "", "Path to client certificate PEM for mTLS (optional)")
	tlsKey        = flag.String("tls-key", "", "Path to client private key PEM for mTLS (optional)")
	tlsSkipVerify = flag.Bool("tls-skip-verify", false, "Skip TLS certificate verification (dev only)")
)

func main() {
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	logger.Info("starting gnmi-tunnel client",
		"target", *target,
		"target_type", *targetType,
		"local_dial_addr", *localDialAddr,
		"tunnel_server_addr", *tunServerAddr,
		"tls", *useTLS,
		"tls_skip_verify", *tlsSkipVerify,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		bo := backoff.NewExponentialBackOff()
		bo.MaxElapsedTime = 0
		bo.InitialInterval = 1 * time.Second
		bo.MaxInterval = 1 * time.Minute
		bo.RandomizationFactor = 0.5
		attempt := 0
		var err error
		for {
			attempt++
			logger.Debug("attempting connection", "attempt", attempt)
			if err = run(ctx, logger); err != nil {
				logger.Error("tunnel client error",
					"error", err,
					"attempt", attempt,
				)
				wait := bo.NextBackOff()
				logger.Info("scheduling reconnect",
					"backoff", wait,
					"next_attempt", attempt+1,
				)
				time.Sleep(wait)
			} else {
				logger.Info("tunnel client disconnected cleanly", "attempt", attempt)
				bo.Reset()
				attempt = 0
			}
			select {
			case <-ctx.Done():
				logger.Info("received shutdown signal, exiting")
				return
			default:
			}
		}
	}()

	<-ctx.Done()
	logger.Info("gnmi-tunnel client shutdown complete")
}

func run(ctx context.Context, logger *slog.Logger) error {
	creds, err := makeTunnelServerCreds(logger)
	if err != nil {
		return fmt.Errorf("failed to configure TLS: %w", err)
	}

	dialTimeout := 30 * time.Second
	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	defer dialCancel()

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithBlock(),
		grpc.WithReturnConnectionError(),
		grpc.FailOnNonTempDialError(true),
	}

	logger.Info("dialing tunnel server",
		"address", *tunServerAddr,
		"timeout", dialTimeout,
	)

	clientConn, err := grpc.DialContext(dialCtx, *tunServerAddr, dialOpts...)
	if err != nil {
		return fmt.Errorf("grpc dial failed: %w", err)
	}
	defer func() {
		logger.Debug("closing gRPC connection", "state", clientConn.GetState().String())
		clientConn.Close()
	}()

	connState := clientConn.GetState()
	if connState != connectivity.Ready {
		logger.Warn("connection not in ready state after dial",
			"address", *tunServerAddr,
			"state", connState.String(),
		)
	} else {
		logger.Info("connected to tunnel server",
			"address", *tunServerAddr,
			"state", connState.String(),
		)
	}

	// Monitor connection state changes in the background
	go func() {
		prevState := connState
		for {
			// Wait for state to change from current state
			if !clientConn.WaitForStateChange(ctx, prevState) {
				// Context cancelled, exit monitor
				return
			}
			newState := clientConn.GetState()
			if newState == connectivity.Ready {
				logger.Info("connection state recovered",
					"from", prevState.String(),
					"to", newState.String(),
				)
			} else if newState == connectivity.TransientFailure {
				logger.Warn("connection state degraded",
					"from", prevState.String(),
					"to", newState.String(),
				)
			} else {
				logger.Debug("connection state changed",
					"from", prevState.String(),
					"to", newState.String(),
				)
			}
			prevState = newState
		}
	}()

	registerHandler := func(t tunnel.Target) error {
		logger.Debug("register handler called",
			"requested_target_id", t.ID,
			"requested_target_type", t.Type,
			"expected_target_id", *target,
		)
		if t.ID == *target {
			logger.Debug("register handler accepted target", "target_id", t.ID)
			return nil
		}
		logger.Warn("register handler rejected target",
			"requested_target_id", t.ID,
			"expected_target_id", *target,
		)
		return fmt.Errorf("could not register handler for target: %s (expected: %s)", t.ID, *target)
	}

	sessionCount := 0
	handler := func(t tunnel.Target, i io.ReadWriteCloser) error {
		sessionCount++
		sessionID := sessionCount
		sessionLogger := logger.With(
			"session_id", sessionID,
			"target_id", t.ID,
			"target_type", t.Type,
		)

		sessionLogger.Info("new tunnel session started")
		startTime := time.Now()

		var dialAddr string
		if t.ID == *target && t.Type == *targetType {
			dialAddr = *localDialAddr
		}
		if len(dialAddr) == 0 {
			sessionLogger.Error("no local dial address for target",
				"expected_type", *targetType,
			)
			return fmt.Errorf("no local port found for target: %s (type: %s)", t.ID, t.Type)
		}

		sessionLogger.Debug("dialing local socket", "dial_addr", dialAddr)
		conn, err := net.Dial("unix", dialAddr)
		if err != nil {
			sessionLogger.Error("failed to dial local socket",
				"dial_addr", dialAddr,
				"error", err,
			)
			return fmt.Errorf("failed to dial %s: %w", dialAddr, err)
		}
		defer conn.Close()

		sessionLogger.Info("session connected to local socket", "dial_addr", dialAddr)
		err = bidi.Copy(i, conn)
		duration := time.Since(startTime)

		if err != nil {
			sessionLogger.Error("session ended with error",
				"duration", duration,
				"error", err,
			)
			return fmt.Errorf("bidi copy error: %w", err)
		}

		sessionLogger.Info("session completed successfully", "duration", duration)
		return nil
	}

	ts := make(map[tunnel.Target]struct{})
	t := tunnel.Target{ID: *target, Type: *targetType}
	ts[t] = struct{}{}

	logger.Debug("creating tunnel client",
		"target_id", t.ID,
		"target_type", t.Type,
	)
	client, err := tunnel.NewClient(tpb.NewTunnelClient(clientConn), tunnel.ClientConfig{
		RegisterHandler: registerHandler,
		Handler:         handler,
	}, ts)
	if err != nil {
		return fmt.Errorf("failed to create tunnel client: %w", err)
	}

	logger.Info("registering with tunnel server",
		"target_id", t.ID,
		"target_type", t.Type,
	)
	if err := client.Register(ctx); err != nil {
		return fmt.Errorf("failed to register tunnel client: %w", err)
	}
	logger.Info("successfully registered with tunnel server",
		"address", *tunServerAddr,
		"target_id", t.ID,
		"target_type", t.Type,
	)

	logger.Debug("starting tunnel client event loop")
	client.Start(ctx)
	logger.Debug("tunnel client event loop exited")
	return client.Error()
}

func makeTunnelServerCreds(logger *slog.Logger) (credentials.TransportCredentials, error) {
	if !*useTLS {
		logger.Debug("TLS disabled, using insecure credentials")
		return insecure.NewCredentials(), nil
	}

	logger.Debug("configuring TLS",
		"skip_verify", *tlsSkipVerify,
		"server_name", *tlsServerName,
		"ca_file", *tlsCA,
		"cert_file", *tlsCert,
		"key_file", *tlsKey,
	)

	tlsCfg := &tls.Config{
		InsecureSkipVerify: *tlsSkipVerify,
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"h2"}, // gRPC uses HTTP/2
	}

	// Set SNI: use explicit override, otherwise extract from address
	if *tlsServerName != "" {
		tlsCfg.ServerName = *tlsServerName
		logger.Debug("TLS server name set from flag", "server_name", *tlsServerName)
	} else if host, _, err := net.SplitHostPort(*tunServerAddr); err == nil {
		tlsCfg.ServerName = host
		logger.Debug("TLS server name extracted from address", "server_name", host)
	} else {
		logger.Debug("could not extract SNI from address", "address", *tunServerAddr)
	}

	if *tlsCA != "" {
		logger.Debug("loading custom CA", "ca_file", *tlsCA)
		b, err := os.ReadFile(*tlsCA)
		if err != nil {
			return nil, fmt.Errorf("read tls-ca %s: %w", *tlsCA, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("tls-ca %s: no valid certs found in PEM", *tlsCA)
		}
		tlsCfg.RootCAs = pool
		logger.Debug("custom CA loaded successfully", "ca_file", *tlsCA)
	}

	if *tlsCert != "" || *tlsKey != "" {
		if *tlsCert == "" || *tlsKey == "" {
			return nil, fmt.Errorf("both --tls-cert and --tls-key are required for mTLS (got cert=%q, key=%q)", *tlsCert, *tlsKey)
		}
		logger.Debug("loading client certificate for mTLS",
			"cert_file", *tlsCert,
			"key_file", *tlsKey,
		)
		cert, err := tls.LoadX509KeyPair(*tlsCert, *tlsKey)
		if err != nil {
			return nil, fmt.Errorf("load client keypair (cert=%s, key=%s): %w", *tlsCert, *tlsKey, err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
		logger.Debug("client certificate loaded for mTLS")
	}

	logger.Debug("TLS configuration complete")
	return credentials.NewTLS(tlsCfg), nil
}
