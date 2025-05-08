package pim_test

import (
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

	var h = []byte{
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
var joinPacket = `01005e00000dc2023d800001080045c00036008b00000167cdfb0a00000ee000000d23005ae501000a00000d000100d201000020ef7b7b7b000100000100072001010101`

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
var prunePacket = `01005e00000dc2023d800001080045c0003601a400000167cce20a00000ee000000d23005ae501000a00000d000100d201000020ef7b7b7b000000010100072001010101`
