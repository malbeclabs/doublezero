package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/malbeclabs/doublezero/tools/uping/pkg/uping"
	"github.com/spf13/pflag"
)

func main() {
	var (
		iface   string
		ipStr   string
		timeout time.Duration
		verbose bool
	)

	pflag.StringVarP(&iface, "iface", "i", "", "interface to bind for RX/TX (required)")
	pflag.StringVarP(&ipStr, "ip", "p", "", "IPv4 source address on that interface (required)")
	pflag.DurationVarP(&timeout, "timeout", "t", 3*time.Second, "poll timeout")
	pflag.BoolVarP(&verbose, "verbose", "v", false, "enable verbose logs")
	pflag.Parse()

	fail := func(msg string, code int) {
		fmt.Fprintf(os.Stderr, "error: %s\n", msg)
		pflag.Usage()
		os.Exit(code)
	}
	if iface == "" {
		fail("missing --iface", 2)
	}
	if ipStr == "" {
		fail("missing --ip", 2)
	}
	if timeout <= 0 {
		fail("--timeout must be > 0", 2)
	}

	ip := mustIPv4(ipStr)

	// Raw sockets + SO_BINDTODEVICE need caps; require if iface provided (always here).
	if err := uping.RequirePrivileges(true); err != nil {
		fmt.Fprintf(os.Stderr, "privileges check failed: %v\n", err)
		os.Exit(1)
	}

	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	log.Info("uping-recv started", "iface", iface, "ip", ip.String(), "timeout", timeout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := uping.NewListener(uping.ListenerConfig{
		Logger:    log,
		Interface: iface,
		IP:        ip,
		Timeout:   timeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create listener: %v\n", err)
		os.Exit(1)
	}

	if err := ln.Listen(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "listen error: %v\n", err)
		os.Exit(1)
	}
}

func mustIPv4(s string) net.IP {
	ip := net.ParseIP(s).To4()
	if ip == nil {
		fmt.Fprintf(os.Stderr, "bad IPv4: %s\n", s)
		os.Exit(2)
	}
	return ip
}
