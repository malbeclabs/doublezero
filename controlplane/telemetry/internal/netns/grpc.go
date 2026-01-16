package netns

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// NewNamespacedGRPCConn returns a new gRPC client connection that performs network
// operations within the context of a given network namespace. It constructs a
// custom dialer using a single-threaded dialer wrapped in RunInNamespace,
// allowing connections to be established from inside the specified namespace.
// DNS resolution (if needed) is performed inside the namespace.
func NewNamespacedGRPCConn(
	ctx context.Context,
	namespace,
	targetAddr string,
	dialOpts ...grpc.DialOption,
) (*grpc.ClientConn, error) {
	host, port, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid target address %q: %w", targetAddr, err)
	}
	if port == "" {
		return nil, fmt.Errorf("invalid target address %q: missing port", targetAddr)
	}

	// Resolve DNS inside namespace if host is not an IP literal
	if net.ParseIP(host) == nil {
		resolved, err := resolveInNamespace(ctx, namespace, host)
		if err != nil {
			return nil, fmt.Errorf("resolve %q in namespace %q: %w", host, namespace, err)
		}
		targetAddr = net.JoinHostPort(resolved, port)
	}

	// Use passthrough since we've already resolved
	grpcTarget := "passthrough:///" + targetAddr

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

	cc, err := grpc.NewClient(grpcTarget, all...)
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

// resolveInNamespace resolves a hostname to an IP address within the given namespace.
func resolveInNamespace(ctx context.Context, namespace, host string) (string, error) {
	var ips []net.IP
	var resolveErr error

	_, err := RunInNamespace(namespace, func() (struct{}, error) {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			resolveErr = err
			return struct{}{}, nil
		}
		for _, addr := range addrs {
			ips = append(ips, addr.IP)
		}
		return struct{}{}, nil
	})
	if err != nil {
		return "", err
	}
	if resolveErr != nil {
		return "", resolveErr
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no addresses found for %q", host)
	}

	// Prefer IPv4 if available
	var result string
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			result = ip4.String()
			break
		}
	}
	if result == "" {
		result = ips[0].String()
	}

	slog.Debug("resolved hostname in namespace", "host", host, "ip", result, "namespace", namespace)
	return result, nil
}

// NewNamespacedGRPCConnFromIP is deprecated, use NewNamespacedGRPCConn instead.
// It returns a new gRPC client connection requiring an IP literal target address.
func NewNamespacedGRPCConnFromIP(
	ctx context.Context,
	namespace,
	targetAddr string,
	dialOpts ...grpc.DialOption,
) (*grpc.ClientConn, error) {
	host, _, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid target address %q: %w", targetAddr, err)
	}
	if net.ParseIP(host) == nil {
		return nil, fmt.Errorf("target must be an IP literal (no DNS), got host %q", host)
	}
	return NewNamespacedGRPCConn(ctx, namespace, targetAddr, dialOpts...)
}
