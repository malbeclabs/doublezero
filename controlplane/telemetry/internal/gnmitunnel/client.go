// Package gnmitunnel provides a gRPC tunnel client for exposing local gNMI/gNOI
// services through a remote tunnel server.
package gnmitunnel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/openconfig/grpctunnel/bidi"
	"github.com/openconfig/grpctunnel/tunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	tpb "github.com/openconfig/grpctunnel/proto/tunnel"
)

// TargetType for the tunnel registration.
type TargetType string

const TargetTypeGNMIGNOI TargetType = "GNMI_GNOI"

// Dialer creates network connections. Allows custom dialers (e.g., namespace-aware).
type Dialer func(ctx context.Context, network, address string) (net.Conn, error)

// GRPCClientConnFactory creates gRPC client connections.
type GRPCClientConnFactory func(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

// TLSConfig holds TLS settings for connecting to the tunnel server.
type TLSConfig struct {
	Enabled    bool   // Use TLS (default: true)
	ServerName string // SNI/hostname verification override
	CAFile     string // Path to CA PEM (optional if publicly trusted)
	CertFile   string // Path to client certificate PEM for mTLS
	KeyFile    string // Path to client private key PEM for mTLS
	SkipVerify bool   // Skip certificate verification (dev only)
}

type Config struct {
	Logger                *slog.Logger
	TargetID              string
	TargetType            TargetType
	LocalDialAddr         string // Unix socket or TCP address of local gNMI server
	TunnelServerAddr      string
	LocalDialer           Dialer                // Optional, defaults to net.Dialer
	GRPCClientConnFactory GRPCClientConnFactory // Optional, defaults to grpc.NewClient
	TLS                   *TLSConfig            // Optional, defaults to TLS enabled
	InitialBackoff        time.Duration         // Optional, defaults to 1s
	MaxBackoff            time.Duration         // Optional, defaults to 1m
}

func (c *Config) setDefaults() {
	if c.LocalDialer == nil {
		c.LocalDialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
	}
	if c.GRPCClientConnFactory == nil {
		c.GRPCClientConnFactory = func(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
			host, port, err := net.SplitHostPort(target)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
			if err != nil || len(ips) == 0 {
				return nil, fmt.Errorf("no ipv4 for %s: %w", host, err)
			}
			var ip4 net.IP
			for _, ip := range ips {
				if ip.To4() != nil {
					ip4 = ip
					break
				}
			}
			if ip4 == nil {
				return nil, fmt.Errorf("no ipv4 for %s", host)
			}

			// Use passthrough since we've already resolved
			grpcTarget := "passthrough:///" + net.JoinHostPort(ip4.String(), port)

			dialer := &net.Dialer{
				Timeout:       30 * time.Second,
				KeepAlive:     30 * time.Second,
				FallbackDelay: -1, // Disable Happy Eyeballs fallback
			}
			opts = append(opts, grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, "tcp", addr)
			}))
			return grpc.NewClient(grpcTarget, opts...)
		}
	}
	if c.TLS == nil {
		c.TLS = &TLSConfig{Enabled: true}
	}
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = time.Minute
	}
}

func (c *Config) makeTransportCredentials(logger *slog.Logger) (credentials.TransportCredentials, error) {
	if !c.TLS.Enabled {
		logger.Debug("TLS disabled, using insecure credentials")
		return insecure.NewCredentials(), nil
	}

	tlsCfg := &tls.Config{
		InsecureSkipVerify: c.TLS.SkipVerify,
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"h2"}, // gRPC uses HTTP/2
	}

	// Set SNI: use explicit override, otherwise extract from address
	if c.TLS.ServerName != "" {
		tlsCfg.ServerName = c.TLS.ServerName
	} else if host, _, err := net.SplitHostPort(c.TunnelServerAddr); err == nil {
		tlsCfg.ServerName = host
	}

	if c.TLS.CAFile != "" {
		b, err := os.ReadFile(c.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read tls-ca %s: %w", c.TLS.CAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("tls-ca %s: no valid certs found in PEM", c.TLS.CAFile)
		}
		tlsCfg.RootCAs = pool
	}

	if c.TLS.CertFile != "" || c.TLS.KeyFile != "" {
		if c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
			return nil, fmt.Errorf("both cert and key required for mTLS (cert=%q, key=%q)", c.TLS.CertFile, c.TLS.KeyFile)
		}
		cert, err := tls.LoadX509KeyPair(c.TLS.CertFile, c.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client keypair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return credentials.NewTLS(tlsCfg), nil
}

type Client struct {
	log *slog.Logger
	cfg *Config
}

// grpcTarget formats an address for grpc.NewClient which requires a URI scheme.
// IP literals work as-is, but hostnames need the dns:/// prefix.
func grpcTarget(addr string) string {
	// Already has a scheme (e.g., dns:///, passthrough://, unix:)
	if strings.Contains(addr, "://") {
		return addr
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	// IP literals don't need a scheme
	if net.ParseIP(host) != nil {
		return addr
	}

	// Hostname requires dns:/// prefix
	return "dns:///" + addr
}

func NewClient(cfg *Config) (*Client, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("gnmitunnel: logger required")
	}
	if cfg.TargetID == "" {
		return nil, fmt.Errorf("gnmitunnel: target ID required")
	}
	if cfg.TargetType == "" {
		return nil, fmt.Errorf("gnmitunnel: target type required")
	}
	if cfg.LocalDialAddr == "" {
		return nil, fmt.Errorf("gnmitunnel: local dial address required")
	}
	if cfg.TunnelServerAddr == "" {
		return nil, fmt.Errorf("gnmitunnel: tunnel server address required")
	}
	cfg.setDefaults()

	return &Client{
		log: cfg.Logger.With("component", "gnmitunnel"),
		cfg: cfg,
	}, nil
}

// Start runs the tunnel client in a goroutine, returning an error channel.
func (c *Client) Start(ctx context.Context, cancel context.CancelFunc) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		defer cancel()
		if err := c.Run(ctx); err != nil {
			c.log.Error("gnmitunnel failed", "error", err)
			errCh <- err
		}
	}()
	return errCh
}

// Run connects to the tunnel server and handles sessions until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	c.log.Info("gnmitunnel starting",
		"target", c.cfg.TargetID,
		"server", c.cfg.TunnelServerAddr,
	)

	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = 0
	bo.InitialInterval = c.cfg.InitialBackoff
	bo.MaxInterval = c.cfg.MaxBackoff

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err := c.connect(ctx); err != nil {
			c.log.Error("connection failed", "error", err)
			wait := bo.NextBackOff()
			c.log.Info("reconnecting", "in", wait)

			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
			}
		} else {
			bo.Reset()
		}
	}
}

func (c *Client) connect(ctx context.Context) error {
	creds, err := c.cfg.makeTransportCredentials(c.log)
	if err != nil {
		return fmt.Errorf("configure TLS: %w", err)
	}

	opts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	conn, err := c.cfg.GRPCClientConnFactory(ctx, c.cfg.TunnelServerAddr, opts...)
	if err != nil {
		return fmt.Errorf("grpc dial: %w", err)
	}
	defer conn.Close()

	c.log.Debug("connected to tunnel server", "tls", c.cfg.TLS.Enabled)

	targets := map[tunnel.Target]struct{}{
		{ID: c.cfg.TargetID, Type: string(c.cfg.TargetType)}: {},
	}

	client, err := tunnel.NewClient(tpb.NewTunnelClient(conn), tunnel.ClientConfig{
		RegisterHandler: func(t tunnel.Target) error {
			if t.ID != c.cfg.TargetID {
				return fmt.Errorf("unexpected target: %s", t.ID)
			}
			return nil
		},
		Handler: func(t tunnel.Target, rwc io.ReadWriteCloser) error {
			return c.handleSession(ctx, t, rwc)
		},
	}, targets)
	if err != nil {
		return fmt.Errorf("create tunnel client: %w", err)
	}

	if err := client.Register(ctx); err != nil {
		return fmt.Errorf("register: %w", err)
	}
	c.log.Info("registered with tunnel server")

	client.Start(ctx)
	return client.Error()
}

func (c *Client) handleSession(ctx context.Context, t tunnel.Target, rwc io.ReadWriteCloser) error {
	if t.ID != c.cfg.TargetID || t.Type != string(c.cfg.TargetType) {
		return fmt.Errorf("unexpected target: %s/%s", t.ID, t.Type)
	}

	network := "unix"
	if len(c.cfg.LocalDialAddr) > 0 && c.cfg.LocalDialAddr[0] != '/' {
		network = "tcp"
	}

	conn, err := c.cfg.LocalDialer(ctx, network, c.cfg.LocalDialAddr)
	if err != nil {
		return fmt.Errorf("dial local: %w", err)
	}

	c.log.Debug("session started", "target", t.ID)
	return bidi.Copy(rwc, conn)
}
