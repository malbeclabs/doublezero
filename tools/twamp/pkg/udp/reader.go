package udp

import (
	"context"
	"log/slog"
	"net"
	"time"
)

// TimestampedReader is an interface that reads UDP packets, returns with a timestamp.
type TimestampedReader interface {
	// Now returns the current time.
	Now() time.Time

	// Read reads a packet from the UDP connection.
	Read(ctx context.Context, buf []byte) (n int, t time.Time, err error)
}

func NewTimestampedReader(log *slog.Logger, conn *net.UDPConn) TimestampedReader {
	kt, err := NewKernelTimestampedReader(log, conn)
	if err == nil {
		log.Debug("Using kernel reader")
		return kt
	}
	log.Debug("Falling back to wallclock reader", "error", err)
	return NewWallclockTimestampedReader(conn)
}
