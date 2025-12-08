package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/spf13/pflag"
)

func main() {
	var (
		target   string
		interval time.Duration
	)

	pflag.StringVarP(&target, "target", "t", "127.0.0.1:9000", "UDP target host:port")
	pflag.DurationVarP(&interval, "interval", "i", 200*time.Millisecond, "interval between packets")
	pflag.Parse()

	addr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		log.Fatalf("resolve: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	log.Printf("sending packets to %s every %s", target, interval)

	i := 0
	for {
		msg := fmt.Sprintf("packet-%d at %s", i, time.Now().Format(time.RFC3339Nano))

		_, err := conn.Write([]byte(msg))
		if err != nil {
			log.Printf("write error: %v", err)
		} else {
			log.Printf("sent: %s", msg)
		}

		i++
		time.Sleep(interval)
	}
}
