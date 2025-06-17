package main

import (
	"encoding/binary"
	"math"
	"net"
	"os"
	"time"
)

func ntpTimestamp(t time.Time) (uint32, uint32) {
	sec := uint32(t.Unix())
	frac := uint32(float64(t.Nanosecond()) * (math.Pow(2, 32) / 1e9))
	return sec, frac
}

func main() {
	r := &net.UDPAddr{IP: net.ParseIP(os.Args[1]), Port: 862}
	c, err := net.DialUDP("udp", nil, r)
	if err != nil {
		panic(err)
	}
	defer c.Close()

	buf := make([]byte, 48)
	binary.BigEndian.PutUint32(buf[0:4], 1)
	sec, frac := ntpTimestamp(time.Now())
	binary.BigEndian.PutUint32(buf[4:8], sec)
	binary.BigEndian.PutUint32(buf[8:12], frac)

	start := time.Now()
	_, err = c.Write(buf)
	if err != nil {
		panic(err)
	}
	err = c.SetReadDeadline(time.Now().Add(time.Second))
	if err != nil {
		panic(err)
	}
	_, err = c.Read(buf)
	if err != nil {
		panic(err)
	}
	rtt := time.Since(start)
	println("RTT:", rtt.String())
}
