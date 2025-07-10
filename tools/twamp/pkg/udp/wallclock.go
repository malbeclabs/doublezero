package udp

import (
	"context"
	"fmt"
	"net"
	"time"
)

type WallclockTimestampedReader struct {
	conn *net.UDPConn
}

func NewWallclockTimestampedReader(conn *net.UDPConn) *WallclockTimestampedReader {
	return &WallclockTimestampedReader{conn: conn}
}

func (c *WallclockTimestampedReader) Now() time.Time {
	return time.Now()
}

func (c *WallclockTimestampedReader) Read(ctx context.Context, buf []byte) (int, time.Time, error) {
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return 0, time.Time{}, fmt.Errorf("error setting read deadline: %w", err)
		}
	}
	n, err := c.conn.Read(buf)
	return n, time.Now(), err
}
