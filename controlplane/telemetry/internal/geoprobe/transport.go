package geoprobe

import (
	"fmt"
	"net"
)

const (
	// MaxUDPPacketSize is the maximum size of a UDP packet we'll accept.
	// This is sized to accommodate LocationOffset messages with reference chains.
	// A typical offset is ~240 bytes; 5-level chain = ~1200 bytes (within MTU).
	MaxUDPPacketSize = 1232
)

func SendOffset(conn *net.UDPConn, addr *net.UDPAddr, offset *LocationOffset) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}
	if addr == nil {
		return fmt.Errorf("address is nil")
	}
	if offset == nil {
		return fmt.Errorf("offset is nil")
	}

	data, err := offset.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal offset: %w", err)
	}

	if len(data) > MaxUDPPacketSize {
		return fmt.Errorf("serialized offset size %d exceeds maximum %d", len(data), MaxUDPPacketSize)
	}

	n, err := conn.WriteToUDP(data, addr)
	if err != nil {
		return fmt.Errorf("failed to send UDP datagram: %w", err)
	}

	if n != len(data) {
		return fmt.Errorf("incomplete write: sent %d bytes, expected %d", n, len(data))
	}

	return nil
}

func ReceiveOffset(conn *net.UDPConn) (*LocationOffset, *net.UDPAddr, error) {
	if conn == nil {
		return nil, nil, fmt.Errorf("connection is nil")
	}

	buf := make([]byte, MaxUDPPacketSize)

	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read UDP datagram: %w", err)
	}

	offset := &LocationOffset{}
	if err := offset.Unmarshal(buf[:n]); err != nil {
		return nil, addr, fmt.Errorf("failed to unmarshal offset from %s: %w", addr, err)
	}

	return offset, addr, nil
}

// NewUDPListener creates a UDP listener on the specified port.
// The listener binds to all interfaces (0.0.0.0).
func NewUDPListener(port int) (*net.UDPConn, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port %d: must be in range 1-65535", port)
	}

	addr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: port,
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to create UDP listener on port %d: %w", port, err)
	}

	return conn, nil
}

// NewUDPConn creates an unbound UDP connection that can send to any address.
func NewUDPConn() (*net.UDPConn, error) {
	addr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: 0, // OS picks an ephemeral port
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to create UDP connection: %w", err)
	}

	return conn, nil
}
