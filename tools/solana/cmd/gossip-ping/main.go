package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/spf13/pflag"
)

type Result struct {
	TCPOk bool
	UDPOk bool
	Err   error
}

func main() {
	var timeout time.Duration
	pflag.DurationVar(&timeout, "timeout", 500*time.Millisecond, "dial timeout")
	pflag.Parse()

	args := pflag.Args()
	if len(args) != 1 {
		fmt.Println("usage: gossipcheck [--timeout=dur] <host:port>")
		os.Exit(1)
	}
	addr := args[0]

	if _, _, err := net.SplitHostPort(addr); err != nil {
		fmt.Println("invalid address; must be host:port")
		os.Exit(1)
	}

	fmt.Printf("checking gossip reachability: %s\n", addr)

	res := checkGossip(addr, timeout)

	fmt.Println()
	fmt.Printf("tcp reachable: %v\n", res.TCPOk)
	fmt.Printf("udp reachable: %v\n", res.UDPOk)

	if res.Err != nil {
		fmt.Printf("error: %v\n", res.Err)
	}
}

func checkGossip(addr string, timeout time.Duration) Result {
	var r Result

	tcp, err := net.DialTimeout("tcp", addr, timeout)
	if err == nil {
		r.TCPOk = true
		tcp.Close()
	}

	udp, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		r.Err = fmt.Errorf("udp: %w", err)
		return r
	}
	defer udp.Close()

	udp.SetDeadline(time.Now().Add(timeout))
	_, err = udp.Write([]byte{0})
	if err == nil {
		r.UDPOk = true
	} else {
		r.Err = fmt.Errorf("udp write: %w", err)
	}

	return r
}
