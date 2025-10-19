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
		src     string
		dst     string
		count   int
		timeout time.Duration
		verbose bool
	)

	pflag.StringVarP(&iface, "iface", "i", "", "bind sender to this interface (required)")
	pflag.StringVarP(&src, "src", "s", "", "source IPv4 address (required)")
	pflag.StringVarP(&dst, "dst", "d", "", "destination IPv4 address (required)")
	pflag.IntVarP(&count, "count", "c", 4, "number of echo requests to send (>0)")
	pflag.DurationVarP(&timeout, "timeout", "t", 5*time.Second, "per-echo timeout (e.g. 800ms, 2s)")
	pflag.BoolVarP(&verbose, "verbose", "v", false, "enable verbose logs")
	pflag.Parse()

	if iface == "" {
		fmt.Fprintln(os.Stderr, "error: --iface is required")
		pflag.Usage()
		os.Exit(2)
	}

	if src == "" || dst == "" {
		fmt.Fprintln(os.Stderr, "error: --src and --dst are required")
		pflag.Usage()
		os.Exit(2)
	}
	if count <= 0 {
		fmt.Fprintln(os.Stderr, "error: --count must be > 0")
		os.Exit(2)
	}
	if timeout <= 0 {
		fmt.Fprintln(os.Stderr, "error: --timeout must be > 0")
		os.Exit(2)
	}

	srcIP := mustIPv4(src)
	dstIP := mustIPv4(dst)

	if err := uping.RequirePrivileges(iface != ""); err != nil {
		fmt.Fprintf(os.Stderr, "privileges check failed: %v\n", err)
		os.Exit(1)
	}

	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sender, err := uping.NewSender(uping.SenderConfig{
		Logger:    log,
		Interface: iface,
		Source:    srcIP,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create sender: %v\n", err)
		os.Exit(1)
	}
	defer sender.Close()

	results, err := sender.Send(ctx, uping.SendConfig{
		Target:  dstIP,
		Count:   count,
		Timeout: timeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "send error: %v\n", err)
		os.Exit(1)
	}

	allOK := true
	for i, r := range results.Results {
		seq := i + 1
		if r.Error != nil {
			allOK = false
			fmt.Printf("seq=%d error=%v\n", seq, r.Error)
			continue
		}
		fmt.Printf("seq=%d rtt=%v\n", seq, r.RTT)
	}
	if !allOK {
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
