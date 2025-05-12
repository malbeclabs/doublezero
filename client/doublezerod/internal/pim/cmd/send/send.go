package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	"github.com/google/gopacket"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"golang.org/x/net/ipv4"
)

var (
	iface = flag.String("iface", "", "interface to use")
)

func main() {
	flag.Parse()
	if *iface == "" {
		log.Fatalf("interface not specified")
	}
	c, err := net.ListenPacket("ip4:103", "0.0.0.0")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer c.Close()

	r, err := ipv4.NewRawConn(c)
	if err != nil {
		log.Fatalf("failed to create raw conn: %v", err)
	}
	defer r.Close()

	intf, err := net.InterfaceByName(*iface)
	if err != nil {
		log.Fatalf("failed to get interface: %v", err)
	}

	allPIMRouters := net.IPAddr{IP: net.IPv4(224, 0, 0, 13)}
	// err = r.JoinGroup(intf, &allPIMRouters)
	// if err != nil {
	// 	log.Fatalf("failed to join group: %v", err)
	// }
	// defer r.LeaveGroup(intf, &allPIMRouters)

	if err := r.SetMulticastInterface(intf); err != nil {
		log.Fatalf("failed to set multicast interface: %v", err)
	}

	opts := gopacket.SerializeOptions{}
	buf := gopacket.NewSerializeBuffer()

	helloMsg := &pim.HelloMessage{
		Holdtime:     105,
		DRPriority:   1,
		GenerationID: 3614426332,
	}
	err = helloMsg.SerializeTo(buf, opts)

	pimHeader := &pim.PIMMessage{
		Header: pim.PIMHeader{
			Version:  2,
			Type:     pim.Hello,
			Checksum: 0x41fe,
		},
	}

	err = pimHeader.SerializeTo(buf, opts)
	fmt.Printf("bytes: %x size: %d\n", buf.Bytes(), len(buf.Bytes()))
	iph := &ipv4.Header{
		Version:  4,
		Len:      20,
		TTL:      1,
		Protocol: 103,
		Dst:      allPIMRouters.IP,
		TotalLen: ipv4.HeaderLen + len(buf.Bytes()),
	}
	cm := &ipv4.ControlMessage{
		IfIndex: intf.Index,
	}
	if err := r.WriteTo(iph, buf.Bytes(), cm); err != nil {
		log.Fatalf("failed to write to IP: %v", err)
	} else {
		log.Printf("wrote bytes %d", len(buf.Bytes()))
	}

}
