package pim

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

var PIMMessageType = gopacket.RegisterLayerType(1666, gopacket.LayerTypeMetadata{Name: "PIM", Decoder: gopacket.DecodeFunc(decodePim)})

func (p PIMMessage) LayerType() gopacket.LayerType { return PIMMessageType }

// func (p PIMMessage) LayerContents() []byte         { return p.Header }
// func (p PIMMessage) LayerPayload() []byte          { return nil }

// Message Type                          Destination
// ---------------------------------------------------------------------
// 0 = Hello                             Multicast to ALL-PIM-ROUTERS
// 1 = Register                          Unicast to RP
// 2 = Register-Stop                     Unicast to source of Register
// 										 packet
// 3 = Join/Prune                        Multicast to ALL-PIM-ROUTERS
// 4 = Bootstrap                         Multicast to ALL-PIM-ROUTERS
// 5 = Assert                            Multicast to ALL-PIM-ROUTERS
// 6 = Graft (used in PIM-DM only)       Unicast to RPF'(S)
// 7 = Graft-Ack (used in PIM-DM only)   Unicast to source of Graft
// 										 packet
// 8 = Candidate-RP-Advertisement        Unicast to Domain's BSR

func decodePim(data []byte, p gopacket.PacketBuilder) error {
	msg := &PIMMessage{}
	msg.Header.Version = data[0] >> 4
	msg.Header.Type = MessageType(data[0] & 0x0F)
	msg.Header.Reserved = data[1]
	msg.Header.Checksum = binary.BigEndian.Uint16(data[2:4])
	msg.Contents = data[0:4]
	msg.Payload = data[4:]
	p.AddLayer(msg)

	switch msg.Header.Type {
	case Hello:
		return p.NextDecoder(gopacket.DecodeFunc(decodePimHelloMessage))
	case JoinPrune:
		return p.NextDecoder(gopacket.DecodeFunc(decodePimJoinPruneMessage))
	default:
		slog.Error("Unexpected or unsupported PIM type", "type", msg.Header.Type)
		return nil
	}
}

func decodeEncodedUnicastAddr(data []byte) (net.IP, error) {
	if len(data) < 2 {
		return nil, errors.New("Encoded Unicast address is too short")
	}
	addrFamily := data[0]
	switch addrFamily {
	case 1: // IPv4
		if len(data[2:]) < 4 {
			return nil, errors.New("IPv4 address is too short")
		}
		return net.IP(data[2:6]), nil
	case 2: // IPv6
		if len(data[2:]) < 16 {
			return nil, errors.New("IPv6 address is too short")
		}
		return net.IP(data[2:18]), nil
	default:
		return nil, errors.New("Unsupported address family")
	}

}

func decodePimHelloMessage(data []byte, p gopacket.PacketBuilder) error {
	hello := &PIMMessage{BaseLayer: layers.BaseLayer{Contents: data}}
	p.AddLayer(hello)
	for len(data) > 0 {
		if len(data) < 4 {
			return errors.New("PIM Hello option is too short")
		}
		option := &HelloOption{}
		option.Type = OptionType(binary.BigEndian.Uint16(data[0:2]))
		option.Length = binary.BigEndian.Uint16(data[2:4])
		// TODO: bounds check based on the actual type of the option
		// if option.Length < 4 {
		// 	return errors.New("PIM Hello option length is too short")
		// }
		// if len(data[4:]) != int(option.Length) {
		// 	return errors.New("PIM Hello option value is too short")
		// }
		fmt.Printf("------type%v--length---%d\n", option.Type, option.Length)
		switch option.Type {

		case OptionTypeHoldtime:

			option.Value = data[4 : 4+option.Length]
			// this option length offset seemed to be off
			fmt.Printf("%[1]v\n", binary.BigEndian.Uint16(option.Value))
			hello.Hello.Holdtime = binary.BigEndian.Uint16(option.Value)
			option.Length = option.Length + 4
		case OptionTypeLANPruneDelay:
			fmt.Printf("prune delay\n")
			option.Value = data[4 : 4+option.Length]
			hello.Hello.PropDelay = binary.BigEndian.Uint16(option.Value[0:2])
			hello.Hello.OverrideeInterval = binary.BigEndian.Uint16(option.Value[2:4])

			// for some reason these don't get picked up unless it's the integer
			// per the pcap it's holdtime, gen id, then state refresh
		case 19: // dr priority
			option.Value = data[4 : 4+option.Length]
			fmt.Printf("DR Priority: %[1]v\n", binary.BigEndian.Uint16(option.Value))
			hello.Hello.DRPriority = binary.BigEndian.Uint32(option.Value[0:4])
			option.Length = option.Length + 4
		case 20: // gen id
			option.Value = data[4 : 4+option.Length]
			fmt.Printf("GEN ID: %[1]v\n", binary.BigEndian.Uint16(option.Value))
			hello.Hello.GenerationID = binary.BigEndian.Uint32(option.Value)
			option.Length = option.Length + 4

		case 21: // state refresh
			option.Value = data[4 : 4+option.Length]
			fmt.Printf("State Refresh: %[1]v\n", binary.BigEndian.Uint16(option.Value))
			hello.Hello.StateRefreshInterval = option.Value[1]
			option.Length = option.Length + 6
		case OptionTypeAddressList:
			hello.Hello.SecondaryAddress = make([]net.IP, 0)
			encodedAddrHeaderLen := 2
			addrLen := 0
			for i := 0; i < len(data); i += addrLen {
				addr, err := decodeEncodedUnicastAddr(option.Value[i:])
				if err != nil {
					return err
				}
				if addr == nil {
					return errors.New("Invalid address")
				}

				hello.Hello.SecondaryAddress = append(hello.Hello.SecondaryAddress, addr)
				addrLen = len(addr) + encodedAddrHeaderLen
			}
		}
		data = data[option.Length:]
	}

	return nil
}

func decodePimJoinPruneMessage(data []byte, p gopacket.PacketBuilder) error {
	return nil
}

/*
PIM Common Header

	 0                   1                   2                   3
	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|PIM Ver| Type  |   Reserved    |           Checksum            |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
*/
type MessageType uint8

const (
	Hello                   = 0x00
	Register                = 0x01
	RegisterStop            = 0x02
	JoinPrune               = 0x03
	Bootstrap               = 0x04
	Assert                  = 0x05
	Graft                   = 0x06
	GraftAck                = 0x07
	CadidateRPAdvertisement = 0x08
)

type PIMHeader struct {
	Version  uint8
	Type     MessageType
	Reserved uint8
	Checksum uint16
}

type PIMMessage struct {
	layers.BaseLayer
	Header PIMHeader
	Hello  HelloMessage
}

/* PIM Hello Message
    0                   1                   2                   3
    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |PIM Ver| Type  |   Reserved    |           Checksum            |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |          OptionType           |         OptionLength          |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |                          OptionValue                          |
   |                              ...                              |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |                               .                               |
   |                               .                               |
   |                               .                               |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |          OptionType           |         OptionLength          |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |                          OptionValue                          |
   |                              ...                              |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
*/

type HelloMessage struct {
	Holdtime             uint16
	PropDelay            uint16
	OverrideeInterval    uint16
	DRPriority           uint32
	GenerationID         uint32
	SecondaryAddress     []net.IP
	StateRefreshInterval uint8
}

type HelloOption struct {
	Type   OptionType
	Length uint16
	Value  []byte
}

type OptionType uint16

const (
	OptionTypeHoldtime      = 0x0001
	OptionTypeLANPruneDelay = 0x0002
	OptionTypeDRPriority    = 0x0019
	OptionTypeGenerationID  = 0x0020
	OptionTypeStateRefresh  = 0x0021
	OptionTypeAddressList   = 0x0024
)

type Holdtime struct {
	Type     uint16
	Length   uint16
	Holdtime uint8
}

type LANPruneDelay struct {
	Type             uint16
	Length           uint16
	PropDelay        uint16
	OverrideInterval uint16
}

type DRPriority struct {
	Type       uint16
	Length     uint16
	DRPriority uint32
}

type GenerationID struct {
	Type         uint16
	Length       uint16
	GenerationID uint32
}

type StateRefreshInterval struct {
	Type     uint16
	Length   uint16
	Version  uint8
	Interval uint8
	Reserved uint16
}

type AddressList struct {
	Type             uint16
	Length           uint16
	SecondaryAddress []net.IP
}

/*
PIM Join/Prune Message

	 0                   1                   2                   3
	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|PIM Ver| Type  |   Reserved    |           Checksum            |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|        Upstream Neighbor Address (Encoded-Unicast format)     |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|  Reserved     | Num groups    |          Holdtime             |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|         Multicast Group Address 1 (Encoded-Group format)      |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|   Number of Joined Sources    |   Number of Pruned Sources    |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|        Joined Source Address 1 (Encoded-Source format)        |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|                             .                                 |
	|                             .                                 |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|        Joined Source Address n (Encoded-Source format)        |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|        Pruned Source Address 1 (Encoded-Source format)        |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|                             .                                 |
	|                             .                                 |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|        Pruned Source Address n (Encoded-Source format)        |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|                           .                                   |
	|                           .                                   |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|         Multicast Group Address m (Encoded-Group format)      |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|   Number of Joined Sources    |   Number of Pruned Sources    |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|        Joined Source Address 1 (Encoded-Source format)        |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|                             .                                 |
	|                             .                                 |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|        Joined Source Address n (Encoded-Source format)        |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|        Pruned Source Address 1 (Encoded-Source format)        |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|                             .                                 |
	|                             .                                 |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|        Pruned Source Address n (Encoded-Source format)        |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
*/
