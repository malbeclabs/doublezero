package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"

	"github.com/google/gopacket"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"golang.org/x/net/ipv4"
)

var iface = flag.String("iface", "", "interface to use")

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
	if err != nil {
		log.Fatalf("failed to serialize PIM hello msg %v", err)
	}
	pimHeader := &pim.PIMMessage{
		Header: pim.PIMHeader{
			Version:  2,
			Type:     pim.Hello,
			Checksum: 0x0000,
		},
	}

	err = pimHeader.SerializeTo(buf, opts)
	if err != nil {
		log.Fatalf("failed to serialize PIM msg header %v", err)
	}
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

	checksum := pim.Checksum(buf.Bytes())
	b := buf.Bytes()
	binary.BigEndian.PutUint16(b[2:4], checksum)

	fmt.Printf("bytes: %X\n", b)
	fmt.Printf("checksum: %X\n", checksum)
	if err := r.WriteTo(iph, b, cm); err != nil {
		log.Fatalf("failed to write to IP: %v", err)
	} else {
		log.Printf("wrote bytes %d", len(b))
	}

	buf = gopacket.NewSerializeBuffer()

	join := &pim.JoinPruneMessage{
		UpstreamNeighborAddress: net.IP([]byte{169, 254, 1, 3}),
		NumGroups:               1,
		Reserved:                0,
		Holdtime:                210,
		Groups: []pim.Group{
			{
				AddressFamily:         1,
				NumJoinedSources:      1,
				NumPrunedSources:      0,
				MaskLength:            32,
				MulticastGroupAddress: net.IP([]byte{239, 0, 0, 3}),
				Joins: []pim.SourceAddress{
					{
						AddressFamily: 1,
						Flags:         7,
						MaskLength:    32,
						EncodingType:  0,
						Address:       net.IP([]byte{11, 0, 0, 0}),
					},
				},
				Prunes: []pim.SourceAddress{},
			},
		}}

	err = join.SerializeTo(buf, opts)
	if err != nil {
		log.Fatalf("failed to serialize PIM JoinPrune msg %v", err)
	}

	pimHeader = &pim.PIMMessage{
		Header: pim.PIMHeader{
			Version:  2,
			Type:     pim.JoinPrune,
			Checksum: 0x0000,
		}}

	err = pimHeader.SerializeTo(buf, opts)
	if err != nil {
		log.Fatalf("failed to serialize PIM msg header %v", err)
	}

	checksum = pim.Checksum(buf.Bytes())
	b = buf.Bytes()
	binary.BigEndian.PutUint16(b[2:4], checksum)

	if err := r.WriteTo(iph, b, cm); err != nil {
		log.Fatalf("failed to write to IP: %v", err)
	} else {
		log.Printf("wrote bytes %d", len(b))
	}
}
