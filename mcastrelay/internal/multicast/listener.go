// Package multicast provides a UDP multicast listener that receives packets
// and broadcasts them to registered subscribers.
package multicast

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// Packet represents a received UDP multicast packet with its timestamp.
type Packet struct {
	Data       []byte
	ReceivedAt time.Time
}

// Listener listens for UDP multicast packets and broadcasts them to subscribers.
type Listener struct {
	log               *slog.Logger
	multicastIP       net.IP
	port              int
	interfaceName     string
	bufferSize        int // per-packet read buffer
	socketBufferSize  int // OS socket receive buffer (SO_RCVBUF)
	readTimeout       time.Duration
	multicastLoopback bool

	mu          sync.RWMutex
	subscribers map[chan<- Packet]struct{}
}

// Config holds configuration for the multicast listener.
type Config struct {
	Logger            *slog.Logger
	MulticastIP       string // e.g., "239.0.0.1"
	Port              int
	InterfaceName     string // optional, e.g., "eth0"
	BufferSize        int    // per-packet read buffer size, default 65535
	SocketBufferSize  int    // OS socket receive buffer (SO_RCVBUF), default 8MB for high throughput
	ReadTimeout       time.Duration
	MulticastLoopback bool // Enable receiving own multicast packets (useful for testing)
}

const (
	// DefaultSocketBufferSize is 8MB, suitable for high-throughput multicast streams.
	// This can be increased up to the system maximum (check /proc/sys/net/core/rmem_max on Linux).
	DefaultSocketBufferSize = 8 * 1024 * 1024
)

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Logger:           slog.Default(),
		MulticastIP:      "239.0.0.1",
		Port:             5000,
		BufferSize:       65535,
		SocketBufferSize: DefaultSocketBufferSize,
		ReadTimeout:      250 * time.Millisecond,
	}
}

// NewListener creates a new multicast Listener with the given configuration.
func NewListener(cfg *Config) (*Listener, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	ip := net.ParseIP(cfg.MulticastIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid multicast IP: %s", cfg.MulticastIP)
	}
	if !ip.IsMulticast() {
		return nil, fmt.Errorf("IP %s is not a multicast address", cfg.MulticastIP)
	}

	bufferSize := cfg.BufferSize
	if bufferSize <= 0 {
		bufferSize = 65535
	}

	socketBufferSize := cfg.SocketBufferSize
	if socketBufferSize <= 0 {
		socketBufferSize = DefaultSocketBufferSize
	}

	readTimeout := cfg.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 250 * time.Millisecond
	}

	return &Listener{
		log:               cfg.Logger,
		multicastIP:       ip,
		port:              cfg.Port,
		interfaceName:     cfg.InterfaceName,
		bufferSize:        bufferSize,
		socketBufferSize:  socketBufferSize,
		readTimeout:       readTimeout,
		multicastLoopback: cfg.MulticastLoopback,
		subscribers:       make(map[chan<- Packet]struct{}),
	}, nil
}

// Subscribe registers a channel to receive packets. The channel should be
// buffered to avoid blocking the broadcast. Returns a function to unsubscribe.
func (l *Listener) Subscribe(ch chan<- Packet) func() {
	l.mu.Lock()
	l.subscribers[ch] = struct{}{}
	l.mu.Unlock()

	return func() {
		l.mu.Lock()
		delete(l.subscribers, ch)
		l.mu.Unlock()
	}
}

// SubscriberCount returns the current number of subscribers.
func (l *Listener) SubscriberCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.subscribers)
}

// broadcast sends a packet to all subscribers without blocking.
func (l *Listener) broadcast(pkt Packet) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for ch := range l.subscribers {
		select {
		case ch <- pkt:
		default:
			// Drop packet if subscriber channel is full
			l.log.Warn("dropping packet for slow subscriber")
		}
	}
}

// Run starts listening for multicast packets and broadcasts them to subscribers.
// It blocks until the context is cancelled.
func (l *Listener) Run(ctx context.Context) error {
	conn, err := l.createConnection()
	if err != nil {
		return fmt.Errorf("failed to create multicast connection: %w", err)
	}
	defer conn.Close()

	l.log.Info("multicast listener started",
		"multicast_ip", l.multicastIP.String(),
		"port", l.port,
		"buffer_size", l.bufferSize,
	)

	buf := make([]byte, l.bufferSize)

	for {
		select {
		case <-ctx.Done():
			l.log.Info("multicast listener shutting down")
			return ctx.Err()
		default:
		}

		// Set read deadline to allow periodic context checks
		if err := conn.SetReadDeadline(time.Now().Add(l.readTimeout)); err != nil {
			l.log.Error("failed to set read deadline", "error", err)
			continue
		}

		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if isTimeout(err) {
				continue // Normal timeout, check context and continue
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			l.log.Error("error reading UDP packet", "error", err)
			continue
		}

		// Timestamp immediately after receiving
		receivedAt := time.Now()

		// Copy data to avoid buffer reuse issues
		data := make([]byte, n)
		copy(data, buf[:n])

		pkt := Packet{
			Data:       data,
			ReceivedAt: receivedAt,
		}

		l.broadcast(pkt)
	}
}

// createConnection creates and configures the UDP multicast connection.
func (l *Listener) createConnection() (*net.UDPConn, error) {
	addr := &net.UDPAddr{
		IP:   l.multicastIP,
		Port: l.port,
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen UDP: %w", err)
	}

	// Join multicast group
	p := ipv4.NewPacketConn(conn)

	var ifi *net.Interface
	if l.interfaceName != "" {
		ifi, err = net.InterfaceByName(l.interfaceName)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to get interface %s: %w", l.interfaceName, err)
		}
	}

	if err := p.JoinGroup(ifi, &net.UDPAddr{IP: l.multicastIP}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to join multicast group: %w", err)
	}

	// Enable multicast loopback if configured (allows receiving own packets, useful for testing)
	if l.multicastLoopback {
		if err := p.SetMulticastLoopback(true); err != nil {
			l.log.Warn("failed to enable multicast loopback", "error", err)
		} else {
			l.log.Debug("multicast loopback enabled")
		}
	}

	// Enable receiving multicast packets
	if err := p.SetControlMessage(ipv4.FlagDst, true); err != nil {
		l.log.Warn("failed to set control message", "error", err)
	}

	// Set socket receive buffer size for high-throughput streams.
	// This helps prevent packet loss when bursts of data arrive faster than
	// the application can process them.
	if err := conn.SetReadBuffer(l.socketBufferSize); err != nil {
		l.log.Warn("failed to set socket receive buffer size",
			"requested", l.socketBufferSize,
			"error", err,
		)
	} else {
		// Log the actual buffer size (kernel may adjust it)
		l.log.Info("socket receive buffer configured",
			"requested", l.socketBufferSize,
		)
	}

	return conn, nil
}

// isTimeout checks if the error is a network timeout.
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
