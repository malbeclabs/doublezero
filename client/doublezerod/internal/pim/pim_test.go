package pim_test

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/gopacket"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
)

/*
Protocol Independent Multicast

	0010 .... = Version: 2
	.... 0000 = Type: Hello (0)
	Reserved byte(s): 00
	Checksum: 0x41fe [correct]
	[Checksum Status: Good]
	PIM Options: 4
	    Option 1: Hold Time: 105
	        Type: 1
	        Length: 2
	        Holdtime: 105
	    Option 20: Generation ID: 3614426332
	        Type: 20
	        Length: 4
	        Generation ID: 3614426332
	    Option 19: DR Priority: 1
	        Type: 19
	        Length: 4
	        DR Priority: 1
	    Option 21: State-Refresh: Version = 1, Interval = 0s
	        Type: 21
	        Length: 4
	        Version: 1
	        Interval: 0
	        Reserved: 0
*/
var helloPacket = []byte{
	0x20, 0x00, 0x41, 0xfe, 0x00, 0x01, 0x00, 0x02,
	0x00, 0x69, 0x00, 0x14, 0x00, 0x04, 0xd7, 0x6f,
	0xc4, 0xdc, 0x00, 0x13, 0x00, 0x04, 0x00, 0x00,
	0x00, 0x01, 0x00, 0x15, 0x00, 0x04, 0x01, 0x00,
	0x00, 0x00,
}

func TestPIMHelloPacket(t *testing.T) {
	p := gopacket.NewPacket(helloPacket, pim.PIMMessageType, gopacket.Default)
	if p.ErrorLayer() != nil {
		t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
	}
	if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
		want := &pim.PIMMessage{
			Header: pim.PIMHeader{
				Version:  2,
				Type:     pim.Hello,
				Checksum: 0x41fe,
			},
		}
		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
			t.Errorf("PIMMessage mismatch (-got +want):\n%s", diff)
		}

		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{}
		err := want.SerializeTo(buf, opts)
		if err != nil {
			t.Fatalf("Error serializing packet: %v", err)
		}
		if diff := cmp.Diff(buf.Bytes(), got.BaseLayer.Contents); diff != "" {
			t.Errorf("Serialized packet mismatch (-got +want):\n%s", diff)
		}
	}
	if got, ok := p.Layer(pim.HelloMessageType).(*pim.HelloMessage); ok {
		want := &pim.HelloMessage{
			Holdtime:     105,
			DRPriority:   1,
			GenerationID: 3614426332,
		}
		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.HelloMessage{}, "BaseLayer")); diff != "" {
			t.Errorf("HelloMessage mismatch (-got +want):\n%s", diff)
		}
	}

	got := &pim.HelloMessage{
		Holdtime:     30,
		DRPriority:   1,
		GenerationID: 3614426332,
	}

	h := []byte{
		0x00, 0x01, 0x00, 0x02, 0x00, 0x1e, // holdtime 30
		0x00, 0x14, 0x00, 0x04, 0xd7, 0x6f, 0xc4, 0xdc, // generation id 3614426332
		0x00, 0x13, 0x00, 0x04, 0x00, 0x00, 0x00, 0x01, // DR Priority 1
	}
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{}
	err := got.SerializeTo(buf, opts)
	if err != nil {
		t.Fatalf("Error serializing packet: %v", err)
	}

	if diff := cmp.Diff(buf.Bytes(), h); diff != "" {
		t.Errorf("Serialized packet mismatch (-got +want):\n%s", diff)
	}
}

/*
Protocol Independent Multicast

	0010 .... = Version: 2
	.... 0011 = Type: Join/Prune (3)
	Reserved byte(s): 00
	Checksum: 0x5ae5 [correct]
	[Checksum Status: Good]
	PIM Options
	    Upstream-neighbor: 10.0.0.13
	        Address Family: IPv4 (1)
	        Encoding Type: Native (0)
	        Unicast: 10.0.0.13
	    Reserved byte(s): 00
	    Num Groups: 1
	    Holdtime: 210
	    Group 0
	        Group 0: 239.123.123.123/32
	            Address Family: IPv4 (1)
	            Encoding Type: Native (0)
	            Flags: 0x00
	                0... .... = Bidirectional PIM: Not set
	                .000 000. = Reserved: 0x00
	                .... ...0 = Admin Scope Zone: Not set
	            Masklen: 32
	            Group: 239.123.123.123
	        Num Joins: 1
	            IP address: 1.1.1.1/32 (SWR)
	                Address Family: IPv4 (1)
	                Encoding Type: Native (0)
	                Flags: 0x07, Sparse, WildCard, Rendezvous Point Tree
	                    0000 0... = Reserved: 0x00
	                    .... .1.. = Sparse: Set
	                    .... ..1. = WildCard: Set
	                    .... ...1 = Rendezvous Point Tree: Set
	                Masklen: 32
	                Source: 1.1.1.1
	        Num Prunes: 0
*/

var joinPacket = []byte{0x23, 0x0, 0x5a, 0xe5, 0x1, 0x0, 0xa, 0x0, 0x0, 0xd, 0x0, 0x1, 0x0, 0xd2, 0x1, 0x0, 0x0, 0x20, 0xef, 0x7b, 0x7b, 0x7b, 0x0, 0x1, 0x0, 0x0, 0x1, 0x0, 0x7, 0x20, 0x1, 0x1, 0x1, 0x1}

func TestPIMJoinPacket(t *testing.T) {
	p := gopacket.NewPacket(joinPacket, pim.PIMMessageType, gopacket.Default)
	if p.ErrorLayer() != nil {
		t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
	}
	if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
		want := &pim.PIMMessage{
			Header: pim.PIMHeader{
				Version:  2,
				Type:     pim.JoinPrune,
				Checksum: 0x5ae5,
			},
		}
		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
			t.Errorf("PIMMessage mismatch (-got +want):\n%s", diff)
		}

		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{}
		err := want.SerializeTo(buf, opts)
		if err != nil {
			t.Fatalf("Error serializing packet: %v", err)
		}
		if diff := cmp.Diff(buf.Bytes(), got.BaseLayer.Contents); diff != "" {
			t.Errorf("Serialized packet mismatch (-got +want):\n%s", diff)
		}
	}
	if got, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage); ok {
		want := &pim.JoinPruneMessage{
			UpstreamNeighborAddress: net.IP([]byte{10, 0, 0, 13}),
			NumGroups:               1,
			Reserved:                0,
			Holdtime:                210,
			Groups: []pim.Group{
				{
					GroupID:               0,
					AddressFamily:         1,
					NumJoinedSources:      1,
					NumPrunedSources:      0,
					MaskLength:            32,
					MulticastGroupAddress: net.IP([]byte{239, 123, 123, 123}),
					Joins: []pim.SourceAddress{
						{AddressFamily: 1,
							Flags:        7,
							MaskLength:   32,
							EncodingType: 0,
							Address:      net.IP([]byte{1, 1, 1, 1}),
						},
					},
					Prunes: []pim.SourceAddress{},
				},
			}}

		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.JoinPruneMessage{}, "BaseLayer")); diff != "" {
			t.Errorf("HelloMessage mismatch (-got +want):\n%s", diff)
		}
	}

	got := &pim.JoinPruneMessage{
		UpstreamNeighborAddress: net.IP([]byte{10, 0, 0, 13}),
		Reserved:                0,
		NumGroups:               1,
		Holdtime:                210,
		Groups: []pim.Group{
			{
				GroupID:               0,
				AddressFamily:         1,
				NumJoinedSources:      1,
				NumPrunedSources:      0,
				MaskLength:            32,
				MulticastGroupAddress: net.IP([]byte{239, 123, 123, 123}),
				Joins: []pim.SourceAddress{
					{AddressFamily: 1,
						Flags:        7,
						MaskLength:   32,
						EncodingType: 0,
						Address:      net.IP([]byte{1, 1, 1, 1}),
					},
				},
				Prunes: []pim.SourceAddress{},
			}}}

	joinPrune := []byte{
		0x1, 0x0, 0xa, 0x0, 0x0, 0xd, // upstream neighbor
		0x0,       // reserved
		0x1,       // num groups 1
		0x0, 0xd2, // holdtime 210
		0x1, 0x0, 0x0, 0x20, 0xef, 0x7b, 0x7b, 0x7b, // group 0
		0x0, 0x1, // numJoinedSources
		0x0, 0x0, // numPrunedSources
		0x1, 0x0, 0x7, 0x20, 0x1, 0x1, 0x1, 0x1, // joins
	}
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{}
	err := got.SerializeTo(buf, opts)
	if err != nil {
		t.Fatalf("Error serializing packet: %v", err)
	}

	if diff := cmp.Diff(buf.Bytes(), joinPrune); diff != "" {
		t.Errorf("Serialized packet mismatch (-got +want):\n%s", diff)
	}

}

/*
Protocol Independent Multicast

	0010 .... = Version: 2
	.... 0011 = Type: Join/Prune (3)
	Reserved byte(s): 00
	Checksum: 0x5ae5 [correct]
	[Checksum Status: Good]
	PIM Options
	    Upstream-neighbor: 10.0.0.13
	        Address Family: IPv4 (1)
	        Encoding Type: Native (0)
	        Unicast: 10.0.0.13
	    Reserved byte(s): 00
	    Num Groups: 1
	    Holdtime: 210
	    Group 0
	        Group 0: 239.123.123.123/32
	            Address Family: IPv4 (1)
	            Encoding Type: Native (0)
	            Flags: 0x00
	                0... .... = Bidirectional PIM: Not set
	                .000 000. = Reserved: 0x00
	                .... ...0 = Admin Scope Zone: Not set
	            Masklen: 32
	            Group: 239.123.123.123
	        Num Joins: 0
	        Num Prunes: 1
	            IP address: 1.1.1.1/32 (SWR)
	                Address Family: IPv4 (1)
	                Encoding Type: Native (0)
	                Flags: 0x07, Sparse, WildCard, Rendezvous Point Tree
	                    0000 0... = Reserved: 0x00
	                    .... .1.. = Sparse: Set
	                    .... ..1. = WildCard: Set
	                    .... ...1 = Rendezvous Point Tree: Set
	                Masklen: 32
	                Source: 1.1.1.1
*/
var prunePacket = []byte{0x23, 0x0, 0x5a, 0xe5, 0x1, 0x0, 0xa, 0x0, 0x0, 0xd, 0x0, 0x1, 0x0, 0xd2, 0x1, 0x0, 0x0, 0x20, 0xef, 0x7b, 0x7b, 0x7b, 0x0, 0x0, 0x0, 0x1, 0x1, 0x0, 0x7, 0x20, 0x1, 0x1, 0x1, 0x1}

func TestPIMPrunePacket(t *testing.T) {
	p := gopacket.NewPacket(prunePacket, pim.PIMMessageType, gopacket.Default)
	if p.ErrorLayer() != nil {
		t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
	}
	if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
		want := &pim.PIMMessage{
			Header: pim.PIMHeader{
				Version:  2,
				Type:     pim.JoinPrune,
				Checksum: 0x5ae5,
			},
		}
		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
			t.Errorf("PIMMessage mismatch (-got +want):\n%s", diff)
		}

		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{}
		err := want.SerializeTo(buf, opts)
		if err != nil {
			t.Fatalf("Error serializing packet: %v", err)
		}
		if diff := cmp.Diff(buf.Bytes(), got.BaseLayer.Contents); diff != "" {
			t.Errorf("Serialized packet mismatch (-got +want):\n%s", diff)
		}
	}
	if got, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage); ok {
		want := &pim.JoinPruneMessage{
			UpstreamNeighborAddress: net.IP([]byte{10, 0, 0, 13}),
			NumGroups:               1,
			Reserved:                0,
			Holdtime:                210,
			Groups: []pim.Group{
				{
					GroupID:               0,
					AddressFamily:         1,
					NumJoinedSources:      0,
					NumPrunedSources:      1,
					MaskLength:            32,
					MulticastGroupAddress: net.IP([]byte{239, 123, 123, 123}),
					Joins:                 []pim.SourceAddress{},
					Prunes: []pim.SourceAddress{
						{AddressFamily: 1,
							Flags:        7,
							MaskLength:   32,
							EncodingType: 0,
							Address:      net.IP([]byte{1, 1, 1, 1})},
					},
				},
			}}

		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.JoinPruneMessage{}, "BaseLayer")); diff != "" {
			t.Errorf("HelloMessage mismatch (-got +want):\n%s", diff)
		}
	}
	got := &pim.JoinPruneMessage{
		Reserved:                0,
		NumGroups:               1,
		Holdtime:                210,
		UpstreamNeighborAddress: net.IP([]byte{10, 0, 0, 13}),
		Groups: []pim.Group{
			{
				GroupID:               0,
				AddressFamily:         1,
				NumJoinedSources:      0,
				NumPrunedSources:      1,
				MaskLength:            32,
				MulticastGroupAddress: net.IP([]byte{239, 123, 123, 123}),
				Joins:                 []pim.SourceAddress{},
				Prunes: []pim.SourceAddress{
					{AddressFamily: 1,
						Flags:        7,
						MaskLength:   32,
						EncodingType: 0,
						Address:      net.IP([]byte{1, 1, 1, 1}),
					},
				},
			},
		},
	}

	joinPrune := []byte{
		0x1, 0x0, 0xa, 0x0, 0x0, 0xd, // upstream neighbor 10.0.0.13
		0x0,       // reserved
		0x1,       // num groups 1
		0x0, 0xd2, // holdtime 210
		0x1, 0x0, 0x0, 0x20, 0xef, 0x7b, 0x7b, 0x7b, // group 0
		0x0, 0x0, // numJoinedSources
		0x0, 0x1, // numPrunedSources
		0x1, 0x0, 0x7, 0x20, 0x1, 0x1, 0x1, 0x1, // prunes
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{}
	err := got.SerializeTo(buf, opts)
	if err != nil {
		t.Fatalf("Error serializing packet: %v", err)
	}

	if diff := cmp.Diff(buf.Bytes(), joinPrune); diff != "" {
		t.Errorf("Serialized packet mismatch (-got +want):\n%s", diff)
	}

}

func TestPIMJoinPrunePacket(t *testing.T) {
	joinPruneMessage := &pim.JoinPruneMessage{
		Reserved:                0,
		NumGroups:               1,
		Holdtime:                210,
		UpstreamNeighborAddress: net.IP([]byte{10, 0, 0, 13}),
		Groups: []pim.Group{
			{
				GroupID:               0,
				AddressFamily:         1,
				NumJoinedSources:      3,
				NumPrunedSources:      3,
				MaskLength:            32,
				MulticastGroupAddress: net.IP([]byte{239, 123, 123, 123}),
				Joins: []pim.SourceAddress{
					{
						AddressFamily: 1,
						Flags:         7,
						MaskLength:    32,
						EncodingType:  0,
						Address:       net.IP([]byte{1, 1, 1, 4}),
					},

					{
						AddressFamily: 1,
						Flags:         7,
						MaskLength:    32,
						EncodingType:  0,
						Address:       net.IP([]byte{1, 1, 1, 5}),
					},
					{
						AddressFamily: 1,
						Flags:         7,
						MaskLength:    32,
						EncodingType:  0,
						Address:       net.IP([]byte{1, 1, 1, 6}),
					},
				},
				Prunes: []pim.SourceAddress{
					{
						AddressFamily: 1,
						Flags:         7,
						MaskLength:    32,
						EncodingType:  0,
						Address:       net.IP([]byte{1, 1, 1, 1}),
					},
					{
						AddressFamily: 1,
						Flags:         7,
						MaskLength:    32,
						EncodingType:  0,
						Address:       net.IP([]byte{1, 1, 1, 2}),
					},
					{
						AddressFamily: 1,
						Flags:         7,
						MaskLength:    32,
						EncodingType:  0,
						Address:       net.IP([]byte{1, 1, 1, 3}),
					},
				},
			},
		},
	}

	joinPrune := []byte{
		0x1, 0x0, 0xa, 0x0, 0x0, 0xd, // upstream neighbor 10.0.0.13
		0x0,       // reserved
		0x1,       // num groups 1
		0x0, 0xd2, // holdtime 210
		0x1, 0x0, 0x0, 0x20, 0xef, 0x7b, 0x7b, 0x7b, // group 0
		0x0, 0x3, // numJoinedSources
		0x0, 0x3, // numPrunedSources
		0x1, 0x0, 0x7, 0x20, 0x1, 0x1, 0x1, 0x4, // join 0
		0x1, 0x0, 0x7, 0x20, 0x1, 0x1, 0x1, 0x5, // join 1
		0x1, 0x0, 0x7, 0x20, 0x1, 0x1, 0x1, 0x6, // join 2
		0x1, 0x0, 0x7, 0x20, 0x1, 0x1, 0x1, 0x1, // prune 0
		0x1, 0x0, 0x7, 0x20, 0x1, 0x1, 0x1, 0x2, // prune 1
		0x1, 0x0, 0x7, 0x20, 0x1, 0x1, 0x1, 0x3, // prune 2
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{}
	err := joinPruneMessage.SerializeTo(buf, opts)

	if err != nil {
		t.Fatalf("Error serializing packet: %v", err)
	}

	if diff := cmp.Diff(buf.Bytes(), joinPrune); diff != "" {
		t.Errorf("Serialized packet mismatch (-got +want):\n%s", diff)
	}

	pimHeader := make([]byte, 4)

	var encoded uint32
	encoded = uint32(2) << 28
	encoded |= uint32(3) << 24
	encoded |= uint32(0) << 16
	encoded |= uint32(23269)
	binary.BigEndian.PutUint32(pimHeader, encoded)

	joinPruneWithHeader := append(pimHeader, joinPrune...)
	p := gopacket.NewPacket(joinPruneWithHeader, pim.PIMMessageType, gopacket.Default)
	if p.ErrorLayer() != nil {
		t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
	}
	if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
		want := &pim.PIMMessage{
			Header: pim.PIMHeader{
				Version:  2,
				Type:     pim.JoinPrune,
				Checksum: 0x5ae5,
			},
		}
		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
			t.Errorf("PIMMessage mismatch (-got +want):\n%s", diff)
		}

		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{}
		err := want.SerializeTo(buf, opts)
		if err != nil {
			t.Fatalf("Error serializing packet: %v", err)
		}
		if diff := cmp.Diff(buf.Bytes(), got.BaseLayer.Contents); diff != "" {
			t.Errorf("Serialized packet mismatch (-got +want):\n%s", diff)
		}
	}

	if got, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage); ok {
		want := joinPruneMessage

		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.JoinPruneMessage{}, "BaseLayer")); diff != "" {
			t.Errorf("HelloMessage mismatch (-got +want):\n%s", diff)
		}
	}

}
