package main

import (
	"flag"
	"io"
	"log"
	"net"
)

func handleConn(src net.Conn, target string) {
	defer src.Close()

	dst, err := net.Dial("tcp", target)
	if err != nil {
		log.Printf("dial %s failed: %v", target, err)
		return
	}
	defer dst.Close()

	done := make(chan struct{}, 2)
	go func() {
		io.Copy(dst, src)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(src, dst)
		done <- struct{}{}
	}()
	<-done
}

func main() {
	listen := flag.String("listen", ":9098", "listen address")
	target := flag.String("target", "", "target host:port")
	flag.Parse()
	if *target == "" {
		log.Fatal("-target is required")
	}

	l, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("listen %s failed: %v", *listen, err)
	}
	log.Printf("listening on %s, forwarding to %s", *listen, *target)

	for {
		c, err := l.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go handleConn(c, *target)
	}
}
