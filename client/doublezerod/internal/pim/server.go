package pim

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/gopacket"
	"golang.org/x/net/ipv4"
)

type RawConner interface {
	WriteTo(h *ipv4.Header, b []byte, cm *ipv4.ControlMessage) error
	Close() error
	SetMulticastInterface(iface *net.Interface) error
	SetControlMessage(cm ipv4.ControlFlags, on bool) error
}
type PIMServer struct {
	iface  string
	groups []net.IP
	done   chan struct{}
	conn   RawConner
	wg     *sync.WaitGroup
}

func NewPIMServer(conn RawConner) *PIMServer {
	return &PIMServer{conn: conn, done: make(chan struct{})}
}

func (s *PIMServer) Start(iface string, tunnelAddr net.IP, groups []net.IP) error {
	s.iface = iface
	s.groups = groups

	intf, err := net.InterfaceByName(s.iface)
	if err != nil {
		return fmt.Errorf("failed to get interface: %v", err)
	}
	if err := s.conn.SetMulticastInterface(intf); err != nil {
		return fmt.Errorf("failed to set multicast interface: %v", err)
	}

	s.wg = &sync.WaitGroup{}
	s.wg.Add(1)
	go func() {
		defer s.conn.Close()
		// send before we start ticker so we don't delay provisioning time by ticker interval
		helloMsgBuf, err := constructHelloMessage()
		if err != nil {
			slog.Error("failed to serialize PIM hello msg", "error", err)
		}
		err = sendMsg(helloMsgBuf, intf, s.conn)
		if err != nil {
			slog.Error("failed to send PIM hello msg", "error", err)
		}
		joinPruneMsgBuf, err := constructJoinPruneMessage(tunnelAddr, groups, net.IP([]byte{11, 0, 0, 0}), nil)
		if err != nil {
			slog.Error("failed to serialize PIM join msg", "error", err)
		}
		err = sendMsg(joinPruneMsgBuf, intf, s.conn)
		if err != nil {
			slog.Error("failed to send PIM join msg", "error", err)
		}

		ticker := time.NewTicker(time.Second * 30)
		for {
			select {
			case <-ticker.C:
				helloMsgBuf, err := constructHelloMessage()
				if err != nil {
					slog.Error("failed to serialize PIM hello msg", "error", err)
				}
				err = sendMsg(helloMsgBuf, intf, s.conn)
				if err != nil {
					slog.Error("failed to send PIM hello msg", "error", err)
				}
				joinPruneMsgBuf, err := constructJoinPruneMessage(tunnelAddr, groups, net.IP([]byte{11, 0, 0, 0}), nil)
				if err != nil {
					slog.Error("failed to serialize PIM join msg", "error", err)
				}
				err = sendMsg(joinPruneMsgBuf, intf, s.conn)
				if err != nil {
					slog.Error("failed to send PIM join msg", "error", err)
				}
			case <-s.done:
				joinPruneMsgBuf, err := constructJoinPruneMessage(tunnelAddr, groups, nil, net.IP([]byte{11, 0, 0, 0}))
				if err != nil {
					slog.Error("failed to serialize PIM prune msg", "error", err)
				}
				err = sendMsg(joinPruneMsgBuf, intf, s.conn)
				if err != nil {
					slog.Error("failed to send PIM prune msg", "error", err)
				}
				s.wg.Done()
				return
			}
		}
	}()
	return nil
}

func (s *PIMServer) Close() error {
	s.done <- struct{}{}
	s.wg.Wait()
	return nil
}

func constructHelloMessage() (gopacket.SerializeBuffer, error) {
	opts := gopacket.SerializeOptions{}
	buf := gopacket.NewSerializeBuffer()

	helloMsg := &HelloMessage{
		Holdtime:     105,
		DRPriority:   1,
		GenerationID: 3614426332,
	}
	err := helloMsg.SerializeTo(buf, opts)
	if err != nil {
		return nil, err
	}
	pimHeader := &PIMMessage{
		Header: PIMHeader{
			Version:  2,
			Type:     Hello,
			Checksum: 0x0000,
		},
	}
	err = pimHeader.SerializeTo(buf, opts)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// TODO: at some point this could require multiple groups with joins/prunes mixed together
func constructJoinPruneMessage(upstreamNeighbor net.IP, multicastGroupAddresses []net.IP, joinSourceAddress net.IP, pruneSourceAddress net.IP) (gopacket.SerializeBuffer, error) {
	numGroups := len(multicastGroupAddresses)
	opts := gopacket.SerializeOptions{}
	buf := gopacket.NewSerializeBuffer()
	groups := constructGroups(multicastGroupAddresses, joinSourceAddress, pruneSourceAddress)

	join := &JoinPruneMessage{
		UpstreamNeighborAddress: upstreamNeighbor,
		NumGroups:               uint8(numGroups),
		Reserved:                0,
		Holdtime:                210,
		Groups:                  groups,
	}

	err := join.SerializeTo(buf, opts)
	if err != nil {
		return nil, err
	}

	pimHeader := &PIMMessage{
		Header: PIMHeader{
			Version:  2,
			Type:     JoinPrune,
			Checksum: 0x0000,
		},
	}

	err = pimHeader.SerializeTo(buf, opts)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func sendMsg(buf gopacket.SerializeBuffer, intf *net.Interface, r RawConner) error {
	allPIMRouters := net.IPAddr{IP: net.IPv4(224, 0, 0, 13)}
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

	checksum := Checksum(buf.Bytes())
	b := buf.Bytes()
	binary.BigEndian.PutUint16(b[2:4], checksum)
	if err := r.WriteTo(iph, b, cm); err != nil {
		return err
	} else {
		return err
	}
}

func constructGroups(ips []net.IP, joinSourceAddress net.IP, pruneSourceAddress net.IP) []Group {
	joins := constructSourceAddress(joinSourceAddress)
	prunes := constructSourceAddress(pruneSourceAddress)
	joinPruneGroups := make([]Group, len(ips))
	numJoins := len(joins)
	numPrunes := len(prunes)
	for i, ip := range ips {
		// TODO: handle different address families
		joinPruneGroups[i] = Group{
			GroupID:               uint8(i),
			AddressFamily:         1,
			NumJoinedSources:      uint16(numJoins),
			NumPrunedSources:      uint16(numPrunes),
			MaskLength:            32,
			MulticastGroupAddress: ip,
			Joins:                 joins,
			Prunes:                prunes,
		}
	}

	return joinPruneGroups
}

func constructSourceAddress(ip net.IP) []SourceAddress {
	if ip == nil {
		return []SourceAddress{}
	} else {
		return []SourceAddress{{
			AddressFamily: 1,
			Flags:         RPTreeBit | SparseBit | WildCardBit,
			MaskLength:    32,
			EncodingType:  0,
			Address:       ip,
		}}
	}
}
