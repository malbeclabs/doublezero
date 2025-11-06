package liveness

import (
	"context"
	"log/slog"
	"net"
	"time"
)

type Receiver struct {
	log      *slog.Logger
	conn     *UDPConn
	handleRx HandleRxFunc
}

type HandleRxFunc func(pkt *ControlPacket, peer Peer)

func NewReceiver(log *slog.Logger, conn *UDPConn, handleRx HandleRxFunc) *Receiver {
	return &Receiver{
		log:      log,
		conn:     conn,
		handleRx: handleRx,
	}
}

func (r *Receiver) Run(ctx context.Context) {
	r.log.Debug("liveness.recv: rx loop started")

	buf := make([]byte, 1500)
	for {
		r.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, remoteAddr, localIP, ifname, err := r.conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					r.log.Debug("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
					return
				default:
					continue
				}
			}
			select {
			case <-ctx.Done():
				r.log.Debug("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
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

		peer := Peer{Interface: ifname, LocalIP: localIP.String(), RemoteIP: remoteAddr.IP.String()}

		r.handleRx(ctrl, peer)
	}
}
