package gnmitunnel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openconfig/grpctunnel/tunnel"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	t.Run("valid config", func(t *testing.T) {
		client, err := NewClient(validConfig())
		require.NoError(t, err)
		require.NotNil(t, client)
	})

	t.Run("missing required fields", func(t *testing.T) {
		cfg := validConfig()
		cfg.TargetID = ""
		_, err := NewClient(cfg)
		require.ErrorContains(t, err, "target ID required")
	})
}

func TestClient_Run_Reconnects(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int64
	cfg := validConfig()
	cfg.GRPCClientConnFactory = func(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		attempts.Add(1)
		return nil, errors.New("connection refused")
	}
	cfg.InitialBackoff = time.Millisecond
	cfg.MaxBackoff = time.Millisecond

	client, err := NewClient(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = client.Run(ctx)
	require.NoError(t, err) // Returns nil on context cancellation
	require.GreaterOrEqual(t, attempts.Load(), int64(1), "should attempt at least one connection")
}

func TestClient_HandleSession(t *testing.T) {
	t.Parallel()

	// Start a local echo server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		if n > 0 {
			_, _ = conn.Write(buf[:n])
		}
	}()

	cfg := validConfig()
	cfg.LocalDialAddr = listener.Addr().String()
	cfg.LocalDialer = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}

	client, err := NewClient(cfg)
	require.NoError(t, err)

	// Simulate tunnel connection
	tunnelConn, localConn := net.Pipe()
	defer tunnelConn.Close()
	defer localConn.Close()

	done := make(chan error, 1)
	go func() {
		done <- client.handleSession(
			context.Background(),
			tunnel.Target{ID: cfg.TargetID, Type: string(cfg.TargetType)},
			localConn,
		)
	}()

	// Send and receive through tunnel
	_, err = tunnelConn.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(tunnelConn, buf)
	require.NoError(t, err)
	require.Equal(t, "ping", string(buf))

	tunnelConn.Close()
	<-done
}

func TestClient_HandleSession_NetworkDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr    string
		network string
	}{
		{"/var/run/gnmi.sock", "unix"},
		{"127.0.0.1:6030", "tcp"},
	}

	for _, tt := range tests {
		var gotNetwork string
		cfg := validConfig()
		cfg.LocalDialAddr = tt.addr
		cfg.LocalDialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			gotNetwork = network
			return nil, errors.New("expected")
		}

		client, _ := NewClient(cfg)
		_ = client.handleSession(context.Background(), tunnel.Target{ID: cfg.TargetID, Type: string(cfg.TargetType)}, &nopRWC{})
		require.Equal(t, tt.network, gotNetwork)
	}
}

func TestGrpcTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"192.168.1.1:443", "192.168.1.1:443"},               // IP literal
		{"[::1]:443", "[::1]:443"},                           // IPv6 literal
		{"dns:///example.com:443", "dns:///example.com:443"}, // already has scheme
		{"passthrough://bufnet", "passthrough://bufnet"},     // passthrough scheme
		{"example.com:443", "dns:///example.com:443"},        // hostname needs prefix
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, grpcTarget(tt.in))
	}
}

func TestDefaultGRPCClientConnFactory_IPv4Resolution(t *testing.T) {
	t.Parallel()

	t.Run("resolves localhost to ipv4", func(t *testing.T) {
		cfg := &Config{
			Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
			TargetID:         "test",
			TargetType:       TargetTypeGNMIGNOI,
			LocalDialAddr:    "/tmp/test.sock",
			TunnelServerAddr: "localhost:443",
			LocalDialer: func(ctx context.Context, network, address string) (net.Conn, error) {
				return nil, errors.New("mock")
			},
			// Don't set GRPCClientConnFactory - use the default
		}
		cfg.setDefaults()

		// The default factory should resolve and return a connection (or fail at dial, not resolution)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, err := cfg.GRPCClientConnFactory(ctx, "localhost:443", grpc.WithTransportCredentials(insecure.NewCredentials()))
		// Should succeed in creating the client (grpc.NewClient is lazy)
		require.NoError(t, err)
		require.NotNil(t, conn)
		conn.Close()
	})

	t.Run("error on invalid target format", func(t *testing.T) {
		cfg := &Config{
			Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
			TargetID:         "test",
			TargetType:       TargetTypeGNMIGNOI,
			LocalDialAddr:    "/tmp/test.sock",
			TunnelServerAddr: "localhost:443",
			LocalDialer: func(ctx context.Context, network, address string) (net.Conn, error) {
				return nil, errors.New("mock")
			},
		}
		cfg.setDefaults()

		ctx := context.Background()
		_, err := cfg.GRPCClientConnFactory(ctx, "no-port-here", grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing port")
	})

	t.Run("error on unresolvable hostname", func(t *testing.T) {
		cfg := &Config{
			Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
			TargetID:         "test",
			TargetType:       TargetTypeGNMIGNOI,
			LocalDialAddr:    "/tmp/test.sock",
			TunnelServerAddr: "localhost:443",
			LocalDialer: func(ctx context.Context, network, address string) (net.Conn, error) {
				return nil, errors.New("mock")
			},
		}
		cfg.setDefaults()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := cfg.GRPCClientConnFactory(ctx, "nonexistent.invalid.domain.test:443", grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.Error(t, err)
		require.Contains(t, err.Error(), "no ipv4")
	})
}

func validConfig() *Config {
	return &Config{
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		TargetID:         "test-device",
		TargetType:       TargetTypeGNMIGNOI,
		LocalDialAddr:    "/var/run/gnmiServer.sock",
		TunnelServerAddr: "localhost:10000",
		LocalDialer: func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, errors.New("mock")
		},
		GRPCClientConnFactory: func(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
			return nil, errors.New("mock")
		},
	}
}

type nopRWC struct{}

func (nopRWC) Read(p []byte) (int, error)  { return 0, io.EOF }
func (nopRWC) Write(p []byte) (int, error) { return len(p), nil }
func (nopRWC) Close() error                { return nil }
