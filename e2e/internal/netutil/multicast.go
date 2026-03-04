//go:build linux

package netutil

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

const maxUdpPayload = 1500

// MulticastListener is a simple multicast monitoring utility that allows
// joining multicast groups and recording per-group statistics.
type MulticastListener struct {
	mu           sync.Mutex
	packetCounts map[string]uint64
	wg           sync.WaitGroup
	cancels      []context.CancelFunc
}

func NewMulticastListener() *MulticastListener {
	return &MulticastListener{
		packetCounts: make(map[string]uint64),
	}
}

func (m *MulticastListener) GetStatistics(group net.IP) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.packetCounts[group.String()]
}

func (m *MulticastListener) JoinGroup(ctx context.Context, group net.IP, port string, ifName string) error {
	ifaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("failed to get network interfaces: %v", err)
	}

	var i *net.Interface
	for _, iface := range ifaces {
		if iface.Name == ifName {
			i = &iface
			break
		}
	}
	if i == nil {
		return fmt.Errorf("interface %s not found", ifName)
	}

	// We need to set the IP_MULTICAST_ALL option to 0 to prevent the kernel from
	// sending multicast packets from all system-wide subscribed groups to all multicast
	// listening sockets. See https://man7.org/linux/man-pages/man7/ip.7.html.
	var lc net.ListenConfig
	lc.Control = func(network, address string, c syscall.RawConn) error {
		var opErr error
		err := c.Control(func(fd uintptr) {
			opErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_MULTICAST_ALL, 0)
			if opErr != nil {
				return
			}
		})
		if err != nil {
			return err
		}
		return opErr
	}

	addr := net.JoinHostPort(group.String(), port)
	c, err := lc.ListenPacket(ctx, "udp4", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", addr, err)
	}
	p := ipv4.NewPacketConn(c)

	if err := p.JoinGroup(i, &net.UDPAddr{IP: group}); err != nil {
		p.Close()
		return fmt.Errorf("failed to join multicast group %s: %v", group, err)
	}
	if err := p.SetControlMessage(ipv4.FlagDst, true); err != nil {
		p.Close()
		return fmt.Errorf("failed to set control message: %v", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancels = append(m.cancels, cancel)
	// Reset stats for this group when joining to ensure fresh counts.
	m.packetCounts[group.String()] = 0
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		<-ctx.Done()
		p.Close()
	}()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		buf := make([]byte, maxUdpPayload)
		for {
			n, cm, src, err := p.ReadFrom(buf)
			if err != nil {
				if ctx.Err() != nil {
					log.Printf("Shutting down multicast listener for group %s on %s.", group.String(), ifName)
					return
				}
				log.Printf("Failed to read from connection for group %s on %s: %v", group.String(), ifName, err)
			}
			m.mu.Lock()
			m.packetCounts[cm.Dst.String()]++
			count := m.packetCounts[cm.Dst.String()]
			m.mu.Unlock()
			log.Printf("Received %d bytes for group %s from src %s (total: %d)", n, cm.Dst, src, count)
		}
	}()
	return nil
}

func (m *MulticastListener) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cancel := range m.cancels {
		cancel()
	}
	m.cancels = nil
	m.packetCounts = make(map[string]uint64)
	m.wg.Wait()
}
