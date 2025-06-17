package main

import (
	"log"
	"net"
)

func main() {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 862})
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	buf := make([]byte, 1500)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		_, err = conn.WriteToUDP(buf[:n], addr)
		if err != nil {
			log.Printf("error writing to UDP: %v", err)
		}
	}
}
