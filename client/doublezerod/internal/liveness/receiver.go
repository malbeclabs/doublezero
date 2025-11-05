package liveness

import (
	"context"
	"log/slog"
	"net"
	"time"
)

type Receiver struct {
	log      *slog.Logger
	conn     *net.UDPConn
	handleRx func(ctrl *ControlPacket, pktSrc *net.UDPAddr, pktDst net.IP, pktIfname string)
}

func NewReceiver(log *slog.Logger, conn *net.UDPConn, handleRx func(ctrl *ControlPacket, pktSrc *net.UDPAddr, pktDst net.IP, pktIfname string)) *Receiver {
	return &Receiver{log: log, conn: conn, handleRx: handleRx}
}

func (r *Receiver) Run(ctx context.Context) {
	r.log.Info("liveness.recv: rx loop started")
	buf := make([]byte, 1500)
	for {
		r.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, pktSrc, pktDst, pktIfname, err := readFromUDP(r.conn, buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					r.log.Info("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
					return
				default:
					continue
				}
			}
			select {
			case <-ctx.Done():
				r.log.Info("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
				return
			default:
				continue
			}
		}

		ctrl, err := UnmarshalControlPacket(buf[:n])
		if err != nil {
			r.log.Error("liveness.recv: error parsing control packet", "error", err)
			continue
		}

		r.handleRx(ctrl, pktSrc, pktDst, pktIfname)
	}
}
