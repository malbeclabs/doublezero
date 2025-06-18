package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

func main() {
	quiet := flag.Bool("q", false, "Quiet mode - only show RTT")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "Usage: twamp-sender [-q] host:port\n")
		os.Exit(1)
	}

	targetAddr := flag.Arg(0)

	// Use null logger if quiet mode
	var log *slog.Logger
	if *quiet {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	} else {
		log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	}

	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid address %s: %v\n", targetAddr, err)
		os.Exit(1)
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid port %s: %v\n", portStr, err)
		os.Exit(1)
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to resolve %s: %v\n", host, err)
		os.Exit(1)
	}
	if len(ips) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no IP addresses found for %s\n", host)
		os.Exit(1)
	}

	targetIP := ips[0]
	udpAddr := &net.UDPAddr{IP: targetIP, Port: int(port)}

	sender, err := twamplight.NewSender(log, udpAddr, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create sender: %v\n", err)
		os.Exit(1)
	}
	defer sender.Close()

	ctx := context.Background()
	rtt, err := sender.Probe(ctx)
	if err != nil {
		if err == twamplight.ErrTimeout {
			fmt.Fprintf(os.Stderr, "Error: timeout\n")
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}

	fmt.Printf("RTT: %v\n", rtt)
}
