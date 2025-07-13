package twamplight

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// Reflector listens for incoming TWAMP probe packets and reflects them back to the sender.
//
// It runs a single-threaded event loop using a UDP socket with a read timeout. The reflector is
// designed for test environments or single-client usage and is not optimized for high concurrency.
//
// Use Run(ctx) to start the reflector; it blocks until the context is cancelled.
// Use Close() to stop the reflector and release the socket.
//
// Reflector is not safe for concurrent use.
type BasicReflector struct {
	log     *slog.Logger
	conn    *net.UDPConn
	timeout time.Duration
	once    sync.Once
}

func NewBasicReflector(log *slog.Logger, addr string, timeout time.Duration) (*BasicReflector, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolve addr: %w", err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on UDP port %d: %w", udpAddr.Port, err)
	}
	return &BasicReflector{
		log:     log,
		conn:    conn,
		timeout: timeout,
	}, nil
}

// Run starts the TWAMP reflector.
//
// It listens on the configured UDP port and reflects back the received packets.
//
// It's a blocking function that will run until the context is done.
func (r *BasicReflector) Run(ctx context.Context) error {
	r.log.Info("Starting TWAMP reflector", "address", r.conn.LocalAddr())

	// Start a goroutine to close the connection when context is cancelled
	go func() {
		<-ctx.Done()
		r.Close()
	}()

	buf := make([]byte, 1500)
	for {
		select {
		case <-ctx.Done():
			r.log.Debug("TWAMP reflector stopped by context", "error", ctx.Err())
			return nil
		default:
		}

		// Set read deadline.
		if r.timeout > 0 {
			err := r.conn.SetReadDeadline(time.Now().Add(r.timeout))
			if err != nil {
				if isClosedErr(err) {
					r.log.Debug("TWAMP reflector socket closed")
					return nil
				}
				return fmt.Errorf("error setting read deadline: %w", err)
			}
		} else if deadline, ok := ctx.Deadline(); ok {
			err := r.conn.SetReadDeadline(deadline)
			if err != nil {
				if isClosedErr(err) {
					r.log.Debug("TWAMP reflector socket closed")
					return nil
				}
				return fmt.Errorf("error setting read deadline: %w", err)
			}
		}

		// Receive packet.
		n, addr, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if isClosedErr(err) {
				r.log.Debug("TWAMP reflector socket closed")
				return nil
			}
			r.log.Error("error reading from UDP", "address", addr, "error", err)
			continue
		}

		// Validate packet size.
		if n != PacketSize {
			r.log.Debug("Received non-TWAMP packet", "address", addr, "length", n, "expected", PacketSize)
			continue
		}

		// Validate packet format.
		_, err = UnmarshalPacket(buf[:n])
		if err != nil {
			r.log.Debug("Received malformed TWAMP packet", "address", addr, "error", err)
			continue
		}

		// Set write deadline.
		if r.timeout > 0 {
			err := r.conn.SetWriteDeadline(time.Now().Add(r.timeout))
			if err != nil {
				r.log.Error("error setting write deadline", "error", err)
				continue
			}
		} else if deadline, ok := ctx.Deadline(); ok {
			err := r.conn.SetWriteDeadline(deadline)
			if err != nil {
				r.log.Error("error setting write deadline", "error", err)
				continue
			}
		}

		// Send response.
		_, err = r.conn.WriteToUDP(buf[:n], addr)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if isClosedErr(err) {
				r.log.Debug("TWAMP reflector socket closed")
				return nil
			}
			r.log.Error("error writing to UDP", "address", addr, "error", err)
			continue
		}
	}
}

// Close closes the TWAMP reflector by closing the listener connection.
func (r *BasicReflector) Close() error {
	var err error
	r.once.Do(func() {
		r.log.Debug("Closing TWAMP reflector")
		err = r.conn.Close()
	})
	return err
}

// LocalAddr returns the address the reflector is listening on.
func (r *BasicReflector) LocalAddr() *net.UDPAddr {
	addr := r.conn.LocalAddr().(*net.UDPAddr)
	ip := addr.IP.To4()
	if ip == nil {
		ip = net.IPv4zero
	}
	return &net.UDPAddr{
		IP:   ip,
		Port: addr.Port,
		Zone: addr.Zone,
	}
}

func isClosedErr(err error) bool {
	return errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection")
}
