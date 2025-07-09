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
	localAddr := flag.String("local-addr", "", "Source address (host:port)")
	remoteAddr := flag.String("remote-addr", "", "Remote address (host:port)")
	flag.Parse()

	if *remoteAddr == "" {
		fmt.Fprintf(os.Stderr, "Usage: twamp-sender [-q] [-local-addr host:port] -remote-addr host:port\n")
		os.Exit(1)
	}

	// Use null logger if quiet mode
	var log *slog.Logger
	if *quiet {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	} else {
		log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	}

	// Parse local address if provided
	var localUDPAddr *net.UDPAddr
	if *localAddr != "" {
		localHost, localPortStr, err := net.SplitHostPort(*localAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid local address %s: %v\n", *localAddr, err)
			os.Exit(1)
		}

		localPort, err := strconv.ParseUint(localPortStr, 10, 16)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid local port %s: %v\n", localPortStr, err)
			os.Exit(1)
		}

		localIPs, err := net.LookupIP(localHost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to resolve local %s: %v\n", localHost, err)
			os.Exit(1)
		}
		if len(localIPs) == 0 {
			fmt.Fprintf(os.Stderr, "Error: no IP addresses found for local %s\n", localHost)
			os.Exit(1)
		}

		localUDPAddr = &net.UDPAddr{IP: localIPs[0], Port: int(localPort)}
	}

	// Parse remote address
	host, portStr, err := net.SplitHostPort(*remoteAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid remote address %s: %v\n", *remoteAddr, err)
		os.Exit(1)
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid remote port %s: %v\n", portStr, err)
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

	remoteUDPAddr := &net.UDPAddr{IP: ips[0], Port: int(port)}

	sender, err := twamplight.NewSender(context.Background(), log, "", localUDPAddr, remoteUDPAddr, 5*time.Second)
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
