package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr, err := net.ResolveUDPAddr("udp", ":9000")
	if err != nil {
		log.Fatalf("resolve udp: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("listen udp: %v", err)
	}
	log.Printf("listening on %s", conn.LocalAddr())

	// Closing the conn will unblock ReadFromUDP immediately
	go func() {
		<-ctx.Done()
		log.Printf("context cancelled, closing UDP socket")
		_ = conn.Close()
	}()

	buf := make([]byte, 65535)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			// If we got here because of shutdown, just exit
			if ctx.Err() != nil {
				log.Printf("shutdown requested, exiting read loop: %v", err)
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Printf("read error: %v", err)
			continue
		}

		data := append([]byte(nil), buf[:n]...)
		go handlePacket(conn, remote, data)
	}
}

func handlePacket(conn *net.UDPConn, remote *net.UDPAddr, data []byte) {
	log.Printf("received %d bytes from %s: %q", len(data), remote.String(), string(data))

	resp := []byte("ok\n")
	if _, err := conn.WriteToUDP(resp, remote); err != nil {
		log.Printf("write error to %s: %v", remote.String(), err)
	}
}
