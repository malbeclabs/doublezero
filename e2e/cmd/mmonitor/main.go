//go:build linux

package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"

	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
)

type groupPortPairs []string

func (g *groupPortPairs) String() string {
	return strings.Join(*g, ", ")
}

func (g *groupPortPairs) Set(value string) error {
	*g = append(*g, value)
	return nil
}

var (
	groups groupPortPairs
	ifName = flag.String("interface", "doublezero1", "Network interface to use for multicast")
)

func main() {
	flag.Var(&groups, "group", "Multicast group and port to join in group:port format. Can be specified multiple times.")
	flag.Parse()

	if len(groups) == 0 {
		log.Fatal("Please specify at least one multicast group using -group <ip:port>")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	listener := netutil.NewMulticastListener()
	for _, group := range groups {
		host, port, err := net.SplitHostPort(group)
		if err != nil {
			log.Fatalf("Invalid group format: %s, expected <ip:port>", group)
		}
		groupIP := net.ParseIP(host)
		if groupIP == nil {
			log.Fatalf("Invalid group IP address: %s", host)
		}

		log.Printf("Joining multicast group %s:%s on interface %s", groupIP, port, *ifName)
		if err := listener.JoinGroup(ctx, groupIP, port, *ifName); err != nil {
			log.Fatalf("Failed to join multicast group %s: %v", group, err)
		}
	}

	<-ctx.Done()
	log.Println("Shutting down...")
	stop()
	listener.Stop()
}
