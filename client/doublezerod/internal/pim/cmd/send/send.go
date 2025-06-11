package main

import (
	"encoding/binary"
	"flag"
	"log"
	"net"

	"github.com/google/gopacket"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"golang.org/x/net/ipv4"
)

var (
	iface            = flag.String("iface", "", "interface to use")
	group            = flag.String("group", "", "multicast group to join/prune")
	upstreamNeighbor = flag.String("upstream", "", "upstream neighbor address (for JoinPrune messages)")
	rpAddress        = flag.String("rp", "10.0.0.0", "RP address (for JoinPrune messages, defaults to 10.0.0.0")
	join             = flag.Bool("join", false, "send a join message")
	prune            = flag.Bool("prune", false, "send a prune message")
	holdtime         = flag.Int("holdtime", 120, "holdtime for JoinPrune messages (default 210 seconds)")
)

func main() {
	flag.Parse()
	if *iface == "" {
		log.Fatalf("interface not specified")
	}

	if *group == "" {
		log.Fatalf("multicast group not specified")
	}

	if *upstreamNeighbor == "" {
		log.Fatalf("upstream neighbor address not specified")
	}

	if !*join && !*prune {
		log.Fatalf("either -join or -prune must be specified")
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

	if err := r.WriteTo(iph, b, cm); err != nil {
		log.Fatalf("failed to write to IP: %v", err)
	} else {
		log.Printf("wrote bytes %d", len(b))
	}

	buf = gopacket.NewSerializeBuffer()

	var msg *pim.JoinPruneMessage
	if *join {
		msg = &pim.JoinPruneMessage{
			UpstreamNeighborAddress: net.ParseIP(*upstreamNeighbor).To4(),
			NumGroups:               1,
			Reserved:                0,
			Holdtime:                uint16(*holdtime),
			Groups: []pim.Group{
				{
					AddressFamily:         1,
					NumJoinedSources:      1,
					NumPrunedSources:      0,
					MaskLength:            32,
					MulticastGroupAddress: net.ParseIP(*group).To4(),
					Joins: []pim.SourceAddress{
						{
							AddressFamily: 1,
							Flags:         7,
							MaskLength:    32,
							EncodingType:  0,
							Address:       net.ParseIP(*rpAddress).To4(),
						},
					},
					Prunes: []pim.SourceAddress{},
				},
			},
		}
	}
	if *prune {
		msg = &pim.JoinPruneMessage{
			UpstreamNeighborAddress: net.ParseIP(*upstreamNeighbor).To4(),
			NumGroups:               1,
			Reserved:                0,
			Holdtime:                120,
			Groups: []pim.Group{
				{
					AddressFamily:         1,
					NumJoinedSources:      0,
					NumPrunedSources:      1,
					MaskLength:            32,
					MulticastGroupAddress: net.ParseIP(*group).To4(),
					Joins:                 []pim.SourceAddress{},
					Prunes: []pim.SourceAddress{
						{
							AddressFamily: 1,
							Flags:         7,
							MaskLength:    32,
							EncodingType:  0,
							Address:       net.ParseIP(*rpAddress).To4(),
						},
					},
				},
			},
		}
	}

	err = msg.SerializeTo(buf, opts)
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
