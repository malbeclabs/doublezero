package netns

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// NewNamespacedGRPCConnFromIP returns a new gRPC client connection that performs network
// operations within the context of a given network namespace. It constructs a
// custom dialer using a single-threaded dialer wrapped in RunInNamespace,
// allowing connections to be established from inside the specified namespace.
// TargetAddr should be an IP address and port, not a hostname. DNS resolution is disabled.
func NewNamespacedGRPCConnFromIP(
	ctx context.Context,
	namespace,
	targetAddr string,
	dialOpts ...grpc.DialOption,
) (*grpc.ClientConn, error) {
	// Validate targetAddr is ip:port
	host, port, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid target address %q: %w", targetAddr, err)
	}
	if net.ParseIP(host) == nil {
		return nil, fmt.Errorf("target must be an IP literal (no DNS), got host %q", host)
	}
	if port == "" {
		return nil, fmt.Errorf("invalid target address %q: missing port", targetAddr)
	}

	// Wrap targetAddr in "passthrough:///ip:port"
	targetAddr = "passthrough:///" + targetAddr

	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		return RunInNamespace(namespace, func() (net.Conn, error) {
			return (&net.Dialer{
				DualStack:     false,
				FallbackDelay: -1,
			}).DialContext(ctx, "tcp", addr)
		})
	}

	base := []grpc.DialOption{
		grpc.WithContextDialer(dialer),
	}
	all := append(base, dialOpts...)

	cc, err := grpc.NewClient(targetAddr, all...)
	if err != nil {
		return nil, err
	}

	// Explicit replacement for WithBlock().
	cc.Connect()
	for {
		if err := ctx.Err(); err != nil {
			_ = cc.Close()
			return nil, err
		}
		st := cc.GetState()
		if st == connectivity.Ready {
			return cc, nil
		}
		if !cc.WaitForStateChange(ctx, st) {
			_ = cc.Close()
			return nil, ctx.Err()
		}
	}
}
