package pim

import (
	"encoding/binary"
	"errors"
	"log/slog"
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

var PIMMessageType = gopacket.RegisterLayerType(1666, gopacket.LayerTypeMetadata{Name: "PIM", Decoder: gopacket.DecodeFunc(decodePim)})
var HelloMessageType = gopacket.RegisterLayerType(1667, gopacket.LayerTypeMetadata{Name: "PIMHelloMessage", Decoder: gopacket.DecodeFunc(decodePimHelloMessage)})

func (p *PIMMessage) LayerType() gopacket.LayerType   { return PIMMessageType }
func (p *HelloMessage) LayerType() gopacket.LayerType { return HelloMessageType }

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
	hello := &HelloMessage{BaseLayer: layers.BaseLayer{Contents: data}}
	for len(data) > 0 {
		if len(data) < 4 {
			return errors.New("PIM Hello option is too short")
		}
		option := &HelloOption{}
		option.Type = OptionType(binary.BigEndian.Uint16(data[0:2]))
		option.Length = binary.BigEndian.Uint16(data[2:4])
		option.Value = data[4 : 4+option.Length]
		// TODO: bounds check based on the actual type of the option
		switch option.Type {
		case OptionTypeHoldtime:
			hello.Holdtime = binary.BigEndian.Uint16(option.Value)
		case OptionTypeLANPruneDelay:
			hello.PropDelay = binary.BigEndian.Uint16(option.Value[0:2])
			hello.OverrideeInterval = binary.BigEndian.Uint16(option.Value[2:4])
		case OptionTypeDRPriority:
			hello.DRPriority = binary.BigEndian.Uint32(option.Value[0:4])
		case OptionTypeGenerationID:
			hello.GenerationID = binary.BigEndian.Uint32(option.Value)
		case OptionTypeStateRefresh:
			hello.StateRefreshInterval = option.Value[1]
		case OptionTypeAddressList:
			hello.SecondaryAddress = make([]net.IP, 0)
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

				hello.SecondaryAddress = append(hello.SecondaryAddress, addr)
				addrLen = len(addr) + encodedAddrHeaderLen
			}
		}
		data = data[4+option.Length:]
	}
	p.AddLayer(hello)
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
	layers.BaseLayer
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
	OptionTypeHoldtime      = 1
	OptionTypeLANPruneDelay = 2
	OptionTypeDRPriority    = 19
	OptionTypeGenerationID  = 20
	OptionTypeStateRefresh  = 21
	OptionTypeAddressList   = 24
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
