package pim

import (
	"encoding/binary"
	"errors"
	"log/slog"
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// Check on this at some point
// https://github.com/google/gopacket/blob/b7d9dbd15ae4c4621a62119b0c682dc23061a8bc/layers/ip4.go#L158
func Checksum(bytes []byte) uint16 {
	// Compute checksum
	var csum uint32
	for i := 0; i < len(bytes); i += 2 {
		csum += uint32(bytes[i]) << 8
		csum += uint32(bytes[i+1])
	}
	for {
		// Break when sum is less or equals to 0xFFFF
		if csum <= 65535 {
			break
		}
		// Add carry to the sum
		csum = (csum >> 16) + uint32(uint16(csum))
	}
	// Flip all the bits
	return ^uint16(csum)
}

var (
	PIMMessageType       = gopacket.RegisterLayerType(1666, gopacket.LayerTypeMetadata{Name: "PIM", Decoder: gopacket.DecodeFunc(decodePim)})
	HelloMessageType     = gopacket.RegisterLayerType(1667, gopacket.LayerTypeMetadata{Name: "PIMHelloMessage", Decoder: gopacket.DecodeFunc(decodePimHelloMessage)})
	JoinPruneMessageType = gopacket.RegisterLayerType(1668, gopacket.LayerTypeMetadata{Name: "PIMJoinPruneMessage", Decoder: gopacket.DecodeFunc(decodePimJoinPruneMessage)})
)

func (p *PIMMessage) LayerType() gopacket.LayerType { return PIMMessageType }
func (p *PIMMessage) SerializeTo(b gopacket.SerializeBuffer, opts gopacket.SerializeOptions) error {
	bytes, err := b.PrependBytes(4)
	if err != nil {
		return err
	}
	var encoded uint32
	encoded = uint32(p.Header.Version) << 28
	encoded |= uint32(p.Header.Type) << 24
	encoded |= uint32(p.Header.Reserved) << 16
	encoded |= uint32(p.Header.Checksum)
	binary.BigEndian.PutUint32(bytes, encoded)
	return nil
}

func (p *HelloMessage) LayerType() gopacket.LayerType { return HelloMessageType }

func (p *HelloMessage) SerializeTo(b gopacket.SerializeBuffer, opts gopacket.SerializeOptions) error {
	holdtime := NewHoldtime(p.Holdtime)
	bytes, err := b.PrependBytes(len(holdtime.Bytes()))
	if err != nil {
		return err
	}
	copy(bytes, holdtime.Bytes())

	genId := NewGenerationID(p.GenerationID)
	bytes, err = b.AppendBytes(len(genId.Bytes()))
	if err != nil {
		return err
	}
	copy(bytes, genId.Bytes())

	drPriority := NewDRPriority(p.DRPriority)
	bytes, err = b.AppendBytes(len(drPriority.Bytes()))
	if err != nil {
		return err
	}
	copy(bytes, drPriority.Bytes())
	return nil
}

func (p *JoinPruneMessage) LayerType() gopacket.LayerType { return JoinPruneMessageType }
func (p *JoinPruneMessage) SerializeTo(b gopacket.SerializeBuffer, opts gopacket.SerializeOptions) error {
	addrBytes := serializeEncodedUnicastAddr(p.UpstreamNeighborAddress)
	bytes, err := b.PrependBytes(len(addrBytes))
	if err != nil {
		return err
	}
	copy(bytes, addrBytes)

	header := make([]byte, 4)
	header[0] = p.Reserved
	header[1] = p.NumGroups
	binary.BigEndian.PutUint16(header[2:4], p.Holdtime)
	bytes, err = b.AppendBytes(4)
	if err != nil {
		return err
	}

	copy(bytes, header)
	for _, group := range p.Groups {
		newGroup := NewGroup(group)
		bytes, err = b.AppendBytes(len(newGroup.Bytes()))
		copy(bytes, newGroup.Bytes())
		if err != nil {
			return err
		}
	}
	return nil
}

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
		return nil, errors.New("encoded Unicast address is too short")
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
		return nil, errors.New("unsupported address family")
	}
}

func decodePimHelloMessage(data []byte, p gopacket.PacketBuilder) error {
	hello := &HelloMessage{BaseLayer: layers.BaseLayer{Contents: data}}
	for len(data) > 0 {
		if len(data) < 4 {
			return errors.New("PIM Hello option is too short")
		}
		option := HelloOption{}
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
	joinPrune := &JoinPruneMessage{BaseLayer: layers.BaseLayer{Contents: data}}
	// TODO: properly check addr family
	addr, err := decodeEncodedUnicastAddr(data[0:])
	if err != nil {
		return err
	}
	if addr == nil {
		return errors.New("Invalid address")
	}
	joinPrune.UpstreamNeighborAddress = addr
	data = data[len(addr)+2:]
	joinPrune.Reserved = data[0]
	joinPrune.NumGroups = data[1]
	joinPrune.Holdtime = binary.BigEndian.Uint16(data[2:4])
	groups := make([]Group, 0)
	joinPrune.Groups, err = decodeGroups(joinPrune.NumGroups, groups, data[4:])
	if err != nil {
		return err
	}

	p.AddLayer(joinPrune)
	return nil
}

func decodeGroups(numGroups uint8, groups []Group, data []byte) ([]Group, error) {
	for i := range int(numGroups) {
		group := Group{}
		group.GroupID = uint8(i)
		group.AddressFamily = data[0]
		group.EncodingType = data[1]
		group.Flags = data[2]
		group.MaskLength = data[3]
		len := 4

		// clean this up
		var addr net.IP
		if data[0] == 1 {
			addr = net.IP(data[4:8])
			len = len + 4
		} else if data[0] == 2 {
			addr = net.IP(data[4:20])
			len = len + 16
		}

		data = data[len:]
		group.MulticastGroupAddress = addr

		group.NumJoinedSources = binary.BigEndian.Uint16(data[0:2])
		group.NumPrunedSources = binary.BigEndian.Uint16(data[2:4])

		data = data[4:]

		group.Joins = make([]SourceAddress, 0)
		for range int(group.NumJoinedSources) {
			// first four bytes of the group
			len = 4
			sourceAddress := SourceAddress{}
			sourceAddress.AddressFamily = data[0]
			sourceAddress.EncodingType = data[1]
			sourceAddress.Flags = data[2]
			sourceAddress.MaskLength = data[3]

			if data[0] == 1 {
				addr = net.IP(data[4:8])
				len = len + 4
			} else if data[0] == 2 {
				addr = net.IP(data[4:20])
				len = len + 16
			}
			sourceAddress.Address = addr
			group.Joins = append(group.Joins, sourceAddress)

			data = data[len:]
		}

		group.Prunes = make([]SourceAddress, 0)
		for range int(group.NumPrunedSources) {
			// first four bytes of the group
			len = 4
			sourceAddress := SourceAddress{}
			sourceAddress.AddressFamily = data[0]
			sourceAddress.EncodingType = data[1]
			sourceAddress.Flags = data[2]
			sourceAddress.MaskLength = data[3]

			// TODO: pull into a function
			if data[0] == 1 {
				addr = net.IP(data[4:8])
				len = len + 4
			} else if data[0] == 2 {
				addr = net.IP(data[4:20])
				len = len + 16
			}
			sourceAddress.Address = addr
			group.Prunes = append(group.Prunes, sourceAddress)
			data = data[len:]
		}
		groups = append(groups, group)
	}
	return groups, nil
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
	Options              []HelloOption
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

//	0                   1                   2                   3
//	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |          Type = 1             |         Length = 2            |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |          Holdtime             |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type Holdtime struct {
	Type     uint16
	Length   uint16
	Holdtime uint16
}

func NewHoldtime(holdtime uint16) Holdtime {
	return Holdtime{
		Type:     OptionTypeHoldtime,
		Length:   2,
		Holdtime: holdtime,
	}
}

func (h Holdtime) Bytes() []byte {
	bytes := make([]byte, 6)
	binary.BigEndian.PutUint16(bytes[0:2], h.Type)
	binary.BigEndian.PutUint16(bytes[2:4], h.Length)
	binary.BigEndian.PutUint16(bytes[4:6], h.Holdtime)
	return bytes
}

//	0                   1                   2                   3
//	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |          Type = 2             |          Length = 4           |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |T|      Propagation_Delay      |      Override_Interval        |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type LANPruneDelay struct {
	Type                    uint16
	Length                  uint16
	JoinSuppressionDisabled bool
	PropDelay               uint16
	OverrideInterval        uint16
}

func NewLANPruneDelay(propDelay, overrideInterval uint16) LANPruneDelay {
	return LANPruneDelay{
		Type:                    OptionTypeLANPruneDelay,
		Length:                  4,
		JoinSuppressionDisabled: false,
		PropDelay:               propDelay,
		OverrideInterval:        overrideInterval,
	}
}

func (l LANPruneDelay) Bytes() []byte {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint16(bytes[0:2], l.Type)
	binary.BigEndian.PutUint16(bytes[2:4], l.Length)
	if l.JoinSuppressionDisabled {
		bytes[0] |= 0x80
	}
	binary.BigEndian.PutUint16(bytes[4:6], l.PropDelay)
	binary.BigEndian.PutUint16(bytes[6:8], l.OverrideInterval)
	return bytes
}

//	0                   1                   2                   3
//	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |          Type = 19            |          Length = 4           |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                         DR Priority                           |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type DRPriority struct {
	Type       uint16
	Length     uint16
	DRPriority uint32
}

func NewDRPriority(drPriority uint32) DRPriority {
	return DRPriority{
		Type:       OptionTypeDRPriority,
		Length:     4,
		DRPriority: drPriority,
	}
}

func (d DRPriority) Bytes() []byte {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint16(bytes[0:2], d.Type)
	binary.BigEndian.PutUint16(bytes[2:4], d.Length)
	binary.BigEndian.PutUint32(bytes[4:8], d.DRPriority)
	return bytes
}

//	0                   1                   2                   3
//	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |          Type = 20            |          Length = 4           |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                       Generation ID                           |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type GenerationID struct {
	Type         uint16
	Length       uint16
	GenerationID uint32
}

func NewGenerationID(genID uint32) GenerationID {
	return GenerationID{
		Type:         OptionTypeGenerationID,
		Length:       4,
		GenerationID: genID,
	}
}

func (g GenerationID) Bytes() []byte {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint16(bytes[0:2], g.Type)
	binary.BigEndian.PutUint16(bytes[2:4], g.Length)
	binary.BigEndian.PutUint32(bytes[4:8], g.GenerationID)
	return bytes
}

//	0                   1                   2                   3
//	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |           Type = 21           |           Length = 4          |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |  Version = 1  |   Interval    |            Reserved           |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type StateRefreshInterval struct {
	Type     uint16
	Length   uint16
	Version  uint8
	Interval uint8
	Reserved uint16
}

func NewStateRefreshInterval(interval uint8) StateRefreshInterval {
	return StateRefreshInterval{
		Type:     OptionTypeStateRefresh,
		Length:   4,
		Version:  1,
		Interval: interval,
	}
}

func (s StateRefreshInterval) Bytes() []byte {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint16(bytes[0:2], s.Type)
	binary.BigEndian.PutUint16(bytes[2:4], s.Length)
	bytes[4] = s.Version
	bytes[5] = s.Interval
	binary.BigEndian.PutUint16(bytes[6:8], s.Reserved)
	return bytes
}

//	0                   1                   2                   3
//	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |          Type = 24            |      Length = <Variable>      |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |         Secondary Address 1 (Encoded-Unicast format)          |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
//	...
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |         Secondary Address N (Encoded-Unicast format)          |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type AddressList struct {
	Type             uint16
	Length           uint16
	SecondaryAddress []net.IP
}

func (a AddressList) NewAddressList(addresses []net.IP) AddressList {
	return AddressList{
		Type:             OptionTypeAddressList,
		Length:           uint16(len(addresses) * 4),
		SecondaryAddress: addresses,
	}
}

func (a AddressList) Bytes() []byte {
	addrs := []byte{}
	for _, addr := range a.SecondaryAddress {
		addrs = append(addrs, serializeEncodedUnicastAddr(addr)...)
	}
	bytes := make([]byte, 4+len(addrs))
	binary.BigEndian.PutUint16(bytes[0:2], a.Type)
	binary.BigEndian.PutUint16(bytes[2:4], a.Length)
	copy(bytes[4:], addrs)
	return bytes
}

func serializeEncodedUnicastAddr(addr net.IP) []byte {
	if len(addr) == 4 {
		return append([]byte{1, 0}, addr...)
	} else if len(addr) == 16 {
		return append([]byte{2, 0}, addr...)
	}
	return nil
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
*/

type JoinPruneMessage struct {
	layers.BaseLayer
	UpstreamNeighborAddress net.IP
	Reserved                uint8
	NumGroups               uint8
	Holdtime                uint16
	Groups                  []Group
}

/*
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
*/

type Group struct {
	GroupID               uint8
	AddressFamily         uint8
	EncodingType          uint8
	Flags                 uint8
	MaskLength            uint8
	MulticastGroupAddress net.IP
	NumJoinedSources      uint16
	NumPrunedSources      uint16
	Joins                 []SourceAddress
	Prunes                []SourceAddress
}

func NewGroup(group Group) Group {
	return Group{
		GroupID:               group.GroupID,
		AddressFamily:         group.AddressFamily,
		EncodingType:          group.EncodingType,
		Flags:                 group.Flags,
		MaskLength:            group.MaskLength,
		MulticastGroupAddress: group.MulticastGroupAddress,
		NumJoinedSources:      group.NumJoinedSources,
		NumPrunedSources:      group.NumPrunedSources,
		Joins:                 group.Joins,
		Prunes:                group.Prunes,
	}
}

func (g Group) Bytes() []byte {
	groupAddress := serializeEncodedUnicastAddr(g.MulticastGroupAddress)
	// drop addr family and encoding type
	groupAddress = groupAddress[2:]

	bytes := make([]byte, 4+len(groupAddress))
	bytes[0] = g.AddressFamily
	bytes[1] = g.EncodingType
	bytes[2] = g.Flags
	bytes[3] = g.MaskLength
	copy(bytes[4:], groupAddress)

	numJoinPruneSources := make([]byte, 4)
	binary.BigEndian.PutUint16(numJoinPruneSources[0:2], g.NumJoinedSources)
	binary.BigEndian.PutUint16(numJoinPruneSources[2:4], g.NumPrunedSources)

	bytes = append(bytes, numJoinPruneSources...)
	for _, join := range g.Joins {
		sourceAddress := NewSourceAddress(join)
		bytes = append(bytes, sourceAddress.Bytes()...)

	}

	for _, prune := range g.Prunes {
		sourceAddress := NewSourceAddress(prune)
		bytes = append(bytes, sourceAddress.Bytes()...)

	}
	return bytes
}

type SourceAddress struct {
	AddressFamily uint8
	EncodingType  uint8
	Flags         uint8
	MaskLength    uint8
	Address       net.IP
}

func NewSourceAddress(s SourceAddress) SourceAddress {
	return SourceAddress{
		AddressFamily: s.AddressFamily,
		EncodingType:  s.EncodingType,
		Flags:         s.Flags,
		MaskLength:    s.MaskLength,
		Address:       s.Address,
	}
}

func (s SourceAddress) Bytes() []byte {
	address := serializeEncodedUnicastAddr(s.Address)
	// drop addr family and encoding type
	address = address[2:]
	bytes := make([]byte, 4+len(s.Address))
	bytes[0] = s.AddressFamily
	bytes[1] = s.EncodingType
	bytes[2] = s.Flags
	bytes[3] = s.MaskLength
	copy(bytes[4:], address)

	return bytes
}

/*
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
