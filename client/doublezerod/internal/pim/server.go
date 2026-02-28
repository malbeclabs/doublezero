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

var (
	// Anycast rendezvous point address used within DoubleZero
	RpAddress = net.IP([]byte{10, 0, 0, 0})
)

const (
	joinHoldtime  = uint16(120) // ask upstream router to keep join state for 120 seconds
	pruneHoldtime = uint16(5)   // ask upstream router to flush join state after 5 seconds
)

type RawConner interface {
	WriteTo(h *ipv4.Header, b []byte, cm *ipv4.ControlMessage) error
	Close() error
	SetMulticastInterface(iface *net.Interface) error
	SetControlMessage(cm ipv4.ControlFlags, on bool) error
}

type PIMServer struct {
	iface      string
	groups     []net.IP
	done       chan struct{}
	conn       RawConner
	wg         *sync.WaitGroup
	tunnelAddr net.IP
	updateCh   chan []net.IP
}

func NewPIMServer() *PIMServer {
	return &PIMServer{
		done:     make(chan struct{}),
		updateCh: make(chan []net.IP),
	}
}

func (s *PIMServer) Start(conn RawConner, iface string, tunnelAddr net.IP, groups []net.IP) error {
	s.iface = iface
	s.groups = groups
	s.conn = conn
	s.tunnelAddr = tunnelAddr

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
		defer s.wg.Done()

		// send before we start ticker so we don't delay provisioning time by ticker interval
		sendHelloAndJoin(intf, s.conn, tunnelAddr, s.groups)

		ticker := time.NewTicker(time.Second * 30)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendHelloAndJoin(intf, s.conn, tunnelAddr, s.groups)
			case newGroups := <-s.updateCh:
				added, removed := ipDiff(s.groups, newGroups)
				if len(removed) > 0 {
					buf, err := constructJoinPruneMessage(tunnelAddr, removed, nil, RpAddress, pruneHoldtime)
					if err != nil {
						slog.Error("failed to serialize PIM prune msg for removed groups", "error", err)
					} else if err := sendMsg(buf, intf, s.conn); err != nil {
						slog.Error("failed to send PIM prune msg for removed groups", "error", err)
					}
				}
				if len(added) > 0 {
					buf, err := constructJoinPruneMessage(tunnelAddr, added, RpAddress, nil, joinHoldtime)
					if err != nil {
						slog.Error("failed to serialize PIM join msg for added groups", "error", err)
					} else if err := sendMsg(buf, intf, s.conn); err != nil {
						slog.Error("failed to send PIM join msg for added groups", "error", err)
					}
				}
				s.groups = newGroups
			case <-s.done:
				joinPruneMsgBuf, err := constructJoinPruneMessage(tunnelAddr, s.groups, nil, RpAddress, pruneHoldtime)
				if err != nil {
					slog.Error("failed to serialize PIM prune msg", "error", err)
				} else if err := sendMsg(joinPruneMsgBuf, intf, s.conn); err != nil {
					slog.Error("failed to send PIM prune msg", "error", err)
				}
				return
			}
		}
	}()
	return nil
}

// UpdateGroups applies a new set of multicast groups in-place without
// restarting the goroutine or connection. The goroutine computes the diff
// and sends targeted join/prune messages for added/removed groups.
func (s *PIMServer) UpdateGroups(groups []net.IP) error {
	s.updateCh <- groups
	return nil
}

func sendHelloAndJoin(intf *net.Interface, conn RawConner, tunnelAddr net.IP, groups []net.IP) {
	helloMsgBuf, err := constructHelloMessage()
	if err != nil {
		slog.Error("failed to serialize PIM hello msg", "error", err)
	} else if err := sendMsg(helloMsgBuf, intf, conn); err != nil {
		slog.Error("failed to send PIM hello msg", "error", err)
	}
	joinPruneMsgBuf, err := constructJoinPruneMessage(tunnelAddr, groups, RpAddress, nil, joinHoldtime)
	if err != nil {
		slog.Error("failed to serialize PIM join msg", "error", err)
	} else if err := sendMsg(joinPruneMsgBuf, intf, conn); err != nil {
		slog.Error("failed to send PIM join msg", "error", err)
	}
}

// ipDiff returns IPs that were added and removed when transitioning from old to new.
func ipDiff(old, new []net.IP) (added, removed []net.IP) {
	oldSet := make(map[string]bool, len(old))
	for _, ip := range old {
		oldSet[ip.String()] = true
	}
	newSet := make(map[string]bool, len(new))
	for _, ip := range new {
		newSet[ip.String()] = true
	}
	for _, ip := range new {
		if !oldSet[ip.String()] {
			added = append(added, ip)
		}
	}
	for _, ip := range old {
		if !newSet[ip.String()] {
			removed = append(removed, ip)
		}
	}
	return
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
func constructJoinPruneMessage(upstreamNeighbor net.IP, multicastGroupAddresses []net.IP, joinSourceAddress net.IP, pruneSourceAddress net.IP, holdtime uint16) (gopacket.SerializeBuffer, error) {
	numGroups := len(multicastGroupAddresses)
	opts := gopacket.SerializeOptions{}
	buf := gopacket.NewSerializeBuffer()
	groups := constructGroups(multicastGroupAddresses, joinSourceAddress, pruneSourceAddress)

	join := &JoinPruneMessage{
		UpstreamNeighborAddress: upstreamNeighbor,
		NumGroups:               uint8(numGroups),
		Reserved:                0,
		Holdtime:                holdtime,
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
	}
	return nil
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
